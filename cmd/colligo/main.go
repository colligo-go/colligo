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
	"strings"
	"syscall"

	"github.com/colligo-go/colligo/checksum"
	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/identity"
	"github.com/colligo-go/colligo/internal/buildinfo"
	"github.com/colligo-go/colligo/manifest"
	jsonout "github.com/colligo-go/colligo/render/json"
	"github.com/colligo-go/colligo/render/text"
	"github.com/colligo-go/colligo/samplesheet"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/transport"
	"github.com/colligo-go/colligo/verifier"
)

const usage = `usage: colligo <command> [flags] <path>...

commands:
  check      facts and assertions of loose files or a directory;
             --samplesheet checks an nf-core samplesheet against the content;
             --source ena:<accession> compares them with what ENA declares
  validate   structural validation of the format only
  explain    the long explanation of a diagnosis id from a report, offline
  identify   name a FASTA or GTF by its content: seqcol digest, GENCODE header, official checksum
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
		return runCheck(args[1:], stdout, stderr, false, nil)
	case "validate":
		return runCheck(args[1:], stdout, stderr, true, nil)
	case "identify":
		table, err := identity.Embedded()
		if err != nil {
			fmt.Fprintf(stderr, "colligo: %v\nthe embedded table is unusable; nothing was identified\n", err)
			return verifier.ExitFatal
		}
		return runIdentify(args[1:], stdout, stderr, table)
	case "explain":
		return runExplain(args[1:], stdout, stderr)
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
	presentation
	jsonOut     bool
	permissive  bool
	recursive   bool
	quick       bool
	crossCheck  bool
	certificate bool
	format      string
	jobs        int
	expected    multiFlag
	samplesheet string
	source      string
}

func newFlagSet(command string, stderr io.Writer, structureOnly bool) (*flag.FlagSet, *settings) {
	s := &settings{}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&s.jsonOut, "json", false, "write the report as JSON")
	s.presentation.bind(fs)
	fs.BoolVar(&s.permissive, "permissive", false, "exit 0 on blocking failures; nothing is hidden")
	fs.BoolVar(&s.recursive, "recursive", false, "descend into subdirectories")
	fs.StringVar(&s.format, "format", "", "declare the format: fastq, fasta, gtf, gzip or bgzf")
	fs.IntVar(&s.jobs, "jobs", 2, "files read at the same time")
	if !structureOnly {
		fs.BoolVar(&s.quick, "quick", false, "read only magic bytes, the tail and a sample of the start; never verified")
		fs.BoolVar(&s.crossCheck, "cross-check", false, "write colligo.sha256 and colligo.b3 for sha256sum -c and b3sum -c")
		fs.BoolVar(&s.certificate, "certificate", false, "write <file>.colligo-cert.json next to each file")
		fs.Var(&s.expected, "expected", "key=value declaration: md5, sha256, blake3, size, reads, bases (one file only)")
		fs.StringVar(&s.samplesheet, "samplesheet", "", "check an nf-core samplesheet (CSV) against the content of the files it lists")
		fs.StringVar(&s.source, "source", "", "compare the files with what a source declares: ena:<accession> (a run, experiment, sample or study)")
	}
	return fs, s
}

func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var paths []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if endedByTerminator(fs, args, len(args)-len(rest)) {
			return append(paths, rest...), nil
		}
		if len(rest) == 0 {
			return paths, nil
		}
		paths = append(paths, rest[0])
		args = rest[1:]
	}
}

func endedByTerminator(fs *flag.FlagSet, args []string, consumed int) bool {
	if consumed == 0 || args[consumed-1] != "--" {
		return false
	}
	if consumed < 2 || !strings.HasPrefix(args[consumed-2], "-") || strings.Contains(args[consumed-2], "=") {
		return true
	}
	f := fs.Lookup(strings.TrimLeft(args[consumed-2], "-"))
	if f == nil {
		return true
	}
	boolean, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && boolean.IsBoolFlag()
}

func runCheck(args []string, stdout, stderr io.Writer, structureOnly bool, net transport.Transport) int {
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
	if err := s.presentation.validate(); err != nil {
		fmt.Fprintf(stderr, "colligo %s: %v\n", command, err)
		return verifier.ExitUsage
	}
	if s.samplesheet != "" && s.source != "" {
		fmt.Fprintf(stderr, "colligo %s: --source and --samplesheet cannot be combined yet\n", command)
		return verifier.ExitUsage
	}
	if s.samplesheet != "" {
		return runSamplesheet(s, paths, stdout, stderr)
	}
	var ref sourceRef
	if s.source != "" {
		if ref, err = parseSourceRef(s.source); err != nil {
			fmt.Fprintf(stderr, "colligo %s: %v\n", command, err)
			return verifier.ExitUsage
		}
		switch {
		case s.quick:
			fmt.Fprintf(stderr, "colligo %s: --quick does not apply to --source: the counts need every file read in full\n", command)
			return verifier.ExitUsage
		case len(s.expected) > 0:
			fmt.Fprintf(stderr, "colligo %s: --expected does not apply to --source: the source declares the values\n", command)
			return verifier.ExitUsage
		}
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

	names, skipped, dirLinks, err := collect(ctx, paths, s.recursive)
	if err != nil {
		return failed(err, stderr)
	}
	counter, finish := s.presentation.startProgress(stderr)
	defer finish()
	opts := verifier.Options{Format: s.format, Expected: expected, Structure: structureOnly, Quick: s.quick, Jobs: s.jobs, Progress: counter}
	var report verifier.Report
	if s.source != "" {
		decl, fetchErr := fetchDeclaration(ctx, ref, net)
		if fetchErr != nil {
			return sourceFailed(fetchErr, ref, stderr)
		}
		report, err = verifier.CheckAgainstSource(ctx, names, decl, opts)
	} else {
		report, err = verifier.CheckAll(ctx, names, opts)
	}
	finish()
	if err != nil {
		return failed(err, stderr)
	}
	report.Skipped, report.SkippedLinks = skipped, dirLinks
	return emit(report, report.ExitCode(s.permissive), s, stdout, stderr)
}

func emit(report verifier.Report, code int, s *settings, stdout, stderr io.Writer) int {
	var err error
	if s.jsonOut {
		err = jsonout.Check(stdout, report, code)
	} else {
		err = text.Check(stdout, report, s.presentation.options(stdout, code, s.permissive))
	}
	if err != nil {
		fmt.Fprintf(stderr, "colligo: %v\n", err)
		return verifier.ExitFatal
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

func runSamplesheet(s *settings, paths []string, stdout, stderr io.Writer) int {
	const command = "check --samplesheet"
	switch {
	case len(paths) > 0:
		fmt.Fprintf(stderr, "colligo %s: the samplesheet lists the files; no other path is accepted\n", command)
	case s.quick:
		fmt.Fprintf(stderr, "colligo %s: --quick does not apply: every listed file is read in full\n", command)
	case len(s.expected) > 0:
		fmt.Fprintf(stderr, "colligo %s: --expected applies to one file, not to a samplesheet\n", command)
	case s.format != "":
		fmt.Fprintf(stderr, "colligo %s: --format does not apply: a samplesheet lists FASTQ files\n", command)
	case s.recursive:
		fmt.Fprintf(stderr, "colligo %s: --recursive does not apply: the samplesheet lists the files\n", command)
	default:
		return checkSamplesheet(s, stdout, stderr)
	}
	return verifier.ExitUsage
}

func checkSamplesheet(s *settings, stdout, stderr io.Writer) int {
	sheet, err := samplesheet.Read(secret.NewName(s.samplesheet))
	if err != nil {
		var malformed *samplesheet.FormatError
		if errors.As(err, &malformed) {
			fmt.Fprintf(stderr, "colligo check --samplesheet: %v\n", err)
			return verifier.ExitUsage
		}
		return failed(err, stderr)
	}
	if err := checksum.Verify(); err != nil {
		fmt.Fprintf(stderr, "colligo: %v\nthe tool or its environment is suspect; nothing was checked\n", err)
		return verifier.ExitFatal
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	counter, finish := s.presentation.startProgress(stderr)
	defer finish()
	report, err := verifier.CheckSamplesheet(ctx, sheet, verifier.Options{Jobs: s.jobs, Progress: counter})
	finish()
	if err != nil {
		return failed(err, stderr)
	}
	return emit(report, report.ExitCode(s.permissive), s, stdout, stderr)
}

func collect(ctx context.Context, paths []string, recursive bool) (names []secret.Name, skipped, dirLinks []string, err error) {
	for _, p := range paths {
		name := secret.NewName(p)
		found, skip, links, err := verifier.Discover(ctx, name, recursive)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", name, err)
		}
		names, skipped, dirLinks = append(names, found...), append(skipped, skip...), append(dirLinks, links...)
	}
	return names, skipped, dirLinks, nil
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
