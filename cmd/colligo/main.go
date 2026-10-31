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
	"github.com/colligo-go/colligo/request"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/transport"
	"github.com/colligo-go/colligo/verifier"
)

const usage = `usage: colligo <command> [flags] <path>...

commands:
  check      facts and assertions of loose files or a directory;
             --files-from reads the paths from a list;
             --samplesheet checks an nf-core samplesheet against the content;
             --source ena compares them with what ENA declares
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
	filesFrom   string
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
	fs.StringVar(&s.filesFrom, "files-from", "", "read the paths to check from this file, one per line; - reads them from standard input")
	if !structureOnly {
		fs.BoolVar(&s.quick, "quick", false, "read only magic bytes, the tail and a sample of the start; never verified")
		fs.BoolVar(&s.crossCheck, "cross-check", false, "write colligo.sha256 and colligo.b3 for sha256sum -c and b3sum -c")
		fs.BoolVar(&s.certificate, "certificate", false, "write <file>.colligo-cert.json next to each file")
		fs.Var(&s.expected, "expected", "key=value declaration: md5, sha256, blake3, size, reads, bases (one file only)")
		fs.StringVar(&s.samplesheet, "samplesheet", "", "check an nf-core samplesheet (CSV) against the content of the files it lists")
		fs.StringVar(&s.source, "source", "", "compare the files with what a source declares: ena:<accession> (a run, experiment, sample or study), an ENA address, or ena alone to deduce the accession of one run from the file names")
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
	return runCheckWith(args, stdout, stderr, structureOnly, net, os.Stdin)
}

func runCheckWith(args []string, stdout, stderr io.Writer, structureOnly bool, net transport.Transport, stdin io.Reader) int {
	command := "check"
	if structureOnly {
		command = "validate"
	}
	fs, s := newFlagSet(command, stderr, structureOnly)
	paths, code, finished := parseCommand(fs, command, args, stderr)
	if finished {
		return code
	}
	if err := s.presentation.validate(); err != nil {
		fmt.Fprintf(stderr, "colligo %s: %v\n", command, err)
		return verifier.ExitUsage
	}
	if err := s.incompatible(len(paths)); err != nil {
		fmt.Fprintf(stderr, "colligo %s: %v\n", command, err)
		return verifier.ExitUsage
	}
	expected, err := verifier.ParseExpected(s.expected)
	if err != nil {
		fmt.Fprintf(stderr, "colligo %s: %v\n", command, err)
		return verifier.ExitUsage
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	in, err := s.inputs(paths, stdin)
	if err == nil {
		var req request.Request
		if req, err = request.Resolve(ctx, in); err == nil {
			return checkRequest(ctx, req, expected, s, command, stdout, stderr, net)
		}
	}
	var usage *request.UsageError
	if errors.As(err, &usage) {
		fmt.Fprintf(stderr, "colligo %s: %v\n", command, usage)
		return verifier.ExitUsage
	}
	return failed(err, stderr)
}

func (s *settings) inputs(paths []string, stdin io.Reader) (request.Inputs, error) {
	names, err := request.NamesFromArguments(paths)
	if err != nil {
		return request.Inputs{}, err
	}
	in := request.Inputs{Paths: names, Recursive: s.recursive, Source: s.source}
	if s.samplesheet != "" {
		sheet := secret.NewName(s.samplesheet)
		in.Samplesheet = &sheet
	}
	switch s.filesFrom {
	case "":
	case "-":
		in.ListStdin = stdin
	default:
		list, err := request.NameFromArgument(s.filesFrom)
		if err != nil {
			return request.Inputs{}, err
		}
		in.ListFile = &list
	}
	return in, nil
}

func (s *settings) incompatible(pathCount int) error {
	switch {
	case s.samplesheet != "" && s.quick:
		return fmt.Errorf("--quick does not apply to --samplesheet: every listed file is read in full")
	case s.samplesheet != "" && len(s.expected) > 0:
		return fmt.Errorf("--expected applies to one file, not to a samplesheet")
	case s.samplesheet != "" && s.format != "":
		return fmt.Errorf("--format does not apply to --samplesheet: a samplesheet lists FASTQ files")
	case s.source != "" && s.quick:
		return fmt.Errorf("--quick does not apply to --source: the counts need every file read in full")
	case s.source != "" && len(s.expected) > 0:
		return fmt.Errorf("--expected does not apply to --source: the source declares the values")
	case s.format != "" && !format.Known(s.format):
		return fmt.Errorf("unknown format %q", s.format)
	case len(s.expected) > 0 && s.filesFrom != "":
		return fmt.Errorf("--expected applies to exactly one file, given as an argument; it cannot be combined with --files-from")
	case len(s.expected) > 0 && pathCount != 1:
		return fmt.Errorf("--expected applies to exactly one file")
	}
	return nil
}

func checkRequest(ctx context.Context, req request.Request, expected verifier.Expected, s *settings, command string, stdout, stderr io.Writer, net transport.Transport) int {
	if err := checksum.Verify(); err != nil {
		fmt.Fprintf(stderr, "colligo: %v\nthe tool or its environment is suspect; nothing was checked\n", err)
		return verifier.ExitFatal
	}
	counter, finish := s.presentation.startProgress(stderr)
	defer finish()
	opts := verifier.Options{Format: s.format, Expected: expected, Structure: command == "validate", Quick: s.quick, Jobs: s.jobs, Progress: counter}
	report, err := verifier.Check(ctx, req, fetcher(net), opts)
	finish()
	if err != nil {
		var fetchFailed *verifier.FetchError
		if errors.As(err, &fetchFailed) {
			return sourceFailed(fetchFailed, stderr)
		}
		return failed(err, stderr)
	}
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

func failed(err error, stderr io.Writer) int {
	var usage *request.UsageError
	if errors.As(err, &usage) {
		fmt.Fprintf(stderr, "colligo: %v\n", usage)
		return verifier.ExitUsage
	}
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
