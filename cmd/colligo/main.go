package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"syscall"

	"github.com/colligo-go/colligo/checksum"
	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/internal/buildinfo"
	"github.com/colligo-go/colligo/manifest"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/verifier"
)

const usage = `usage: colligo <command> [flags] <path>...

commands:
  check      facts and assertions of loose files or a directory
  validate   structural validation of the format only
  version    print the version

run "colligo <command> -h" for the flags of a command.
`

type multiFlag []string

func (m *multiFlag) String() string     { return fmt.Sprint([]string(*m)) }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return verifier.ExitUsage
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], stdout, stderr, false)
	case "validate":
		return runCheck(args[1:], stdout, stderr, true)
	case "version", "--version":
		fmt.Fprintf(stdout, "colligo %s (%s)\n", buildinfo.Version(), buildinfo.GitSHA())
		return verifier.ExitOK
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return verifier.ExitOK
	}
	fmt.Fprintf(stderr, "colligo: unknown command %q\n\n%s", args[0], usage)
	return verifier.ExitUsage
}

type settings struct {
	jsonOut     bool
	permissive  bool
	recursive   bool
	quick       bool
	crossCheck  bool
	certificate bool
	format      string
	jobs        int
	expected    multiFlag
}

func newFlagSet(command string, stderr io.Writer, structureOnly bool) (*flag.FlagSet, *settings) {
	s := &settings{}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&s.jsonOut, "json", false, "write the report as JSON")
	fs.BoolVar(&s.permissive, "permissive", false, "exit 0 on blocking failures; nothing is hidden")
	fs.BoolVar(&s.recursive, "recursive", false, "descend into subdirectories")
	fs.StringVar(&s.format, "format", "", "declare the format: fastq, fasta, gtf, gzip or bgzf")
	fs.IntVar(&s.jobs, "jobs", 2, "files read at the same time")
	if !structureOnly {
		fs.BoolVar(&s.quick, "quick", false, "read only magic bytes, the tail and a sample of the start; never verified")
		fs.BoolVar(&s.crossCheck, "cross-check", false, "write colligo.sha256 and colligo.b3 for sha256sum -c and b3sum -c")
		fs.BoolVar(&s.certificate, "certificate", false, "write <file>.colligo-cert.json next to each file")
		fs.Var(&s.expected, "expected", "key=value declaration: md5, sha256, blake3, size, reads, bases (one file only)")
	}
	return fs, s
}

func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var paths []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return paths, nil
		}
		paths = append(paths, args[0])
		args = args[1:]
	}
}

func runCheck(args []string, stdout, stderr io.Writer, structureOnly bool) int {
	command := "check"
	if structureOnly {
		command = "validate"
	}
	fs, s := newFlagSet(command, stderr, structureOnly)
	paths, err := parseInterleaved(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return verifier.ExitOK
		}
		return verifier.ExitUsage
	}
	if len(paths) == 0 {
		fmt.Fprintf(stderr, "colligo %s: no path given\n", command)
		return verifier.ExitUsage
	}
	if s.format != "" && !format.Known(s.format) {
		fmt.Fprintf(stderr, "colligo %s: unknown format %q\n", command, s.format)
		return verifier.ExitUsage
	}
	expected, err := verifier.ParseExpected(s.expected)
	if err != nil {
		fmt.Fprintf(stderr, "colligo %s: %v\n", command, err)
		return verifier.ExitUsage
	}
	if len(s.expected) > 0 && len(paths) != 1 {
		fmt.Fprintf(stderr, "colligo %s: --expected applies to exactly one file\n", command)
		return verifier.ExitUsage
	}
	if err := checksum.Verify(); err != nil {
		fmt.Fprintf(stderr, "colligo: %v\nthe tool or its environment is suspect; nothing was checked\n", err)
		return verifier.ExitFatal
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	names, skipped, err := collect(ctx, paths, s.recursive)
	if err != nil {
		return failed(err, stderr)
	}
	report, err := verifier.CheckAll(ctx, names, verifier.Options{
		Format: s.format, Expected: expected, Structure: structureOnly, Quick: s.quick, Jobs: s.jobs,
	})
	if err != nil {
		return failed(err, stderr)
	}
	report.Skipped = skipped
	code := report.ExitCode(s.permissive)

	if s.jsonOut {
		if err := report.WriteJSON(stdout, code); err != nil {
			fmt.Fprintf(stderr, "colligo: %v\n", err)
			return verifier.ExitFatal
		}
	} else {
		report.WriteText(stdout)
	}
	if s.crossCheck {
		if err := writeCrossCheck(report); err != nil {
			fmt.Fprintf(stderr, "colligo: %v\n", err)
			return verifier.ExitFatal
		}
	}
	if s.certificate {
		for _, f := range report.Files {
			if !manifest.Certifiable(f) {
				continue
			}
			if err := manifest.Write(f); err != nil {
				fmt.Fprintf(stderr, "colligo: certificate for %s: %v\n", f.Name, err)
				return verifier.ExitFatal
			}
		}
	}
	return code
}

func collect(ctx context.Context, paths []string, recursive bool) ([]secret.Name, []string, error) {
	var names []secret.Name
	var skipped []string
	for _, p := range paths {
		name := secret.NewName(p)
		found, skip, err := verifier.Discover(ctx, name, recursive)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		}
		names, skipped = append(names, found...), append(skipped, skip...)
	}
	return names, skipped, nil
}

func failed(err error, stderr io.Writer) int {
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "colligo: interrupted; nothing was saved and nothing is half-done")
		return verifier.ExitInterrupted
	}
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(stderr, "colligo: %v\n", err)
		return verifier.ExitUsage
	}
	fmt.Fprintf(stderr, "colligo: %v\n", err)
	return verifier.ExitFatal
}

func writeCrossCheck(report verifier.Report) error {
	sha, err := os.Create("colligo.sha256")
	if err != nil {
		return err
	}
	b3, err := os.Create("colligo.b3")
	if err != nil {
		sha.Close()
		return err
	}
	if err := report.WriteChecksums(sha, b3); err != nil {
		sha.Close()
		b3.Close()
		return err
	}
	if err := sha.Close(); err != nil {
		b3.Close()
		return err
	}
	return b3.Close()
}
