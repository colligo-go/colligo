package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/colligo-go/colligo/internal/suggest"
	"github.com/colligo-go/colligo/verifier"
)

const exitCodes = `exit codes:
  0  nothing wrong was found
  1  usage error: nothing was read
  2  fatal: a file could not be read, or does not match what was declared
  3  blocking: a problem that stops the data from being called checked
  4  interrupted
  5  --quick: part of the content was not read, so nothing is verified`

type helpText struct {
	does     string
	examples []string
}

var helpTexts = map[string]helpText{
	"check": {
		does: "Reads each file once and says whether the data is what it should be: complete, well formed, and the mates of a pair agree.\nWith --source it also compares the files with what the archive declares for them.",
		examples: []string{
			"colligo check .                          check every data file of this folder",
			"colligo check --source ena .             compare them with ENA; the accession is deduced from the file names",
			"colligo check --source https://www.ebi.ac.uk/ena/browser/view/DRR021374 .",
			"colligo check --files-from files.txt     check the files listed one per line",
			"find . -name '*.fastq.gz' | colligo check --files-from -",
			"colligo check --samplesheet samples.csv  check an nf-core samplesheet against the files it lists",
		},
	},
	"validate": {
		does: "Checks only that each file is well formed (its format and structure), with no checksums and no comparison with a source.",
		examples: []string{
			"colligo validate reads.fastq.gz",
			"colligo validate --files-from files.txt",
		},
	},
	"identify": {
		does: "Names a FASTA or GTF file by its content: the sequence collection digest, the GENCODE header, an official checksum.",
		examples: []string{
			"colligo identify genome.fa.gz",
			"colligo identify --json annotation.gtf.gz",
		},
	},
	"explain": {
		does: "Prints the long explanation of a diagnosis id from a report: what it means, why it happens and what to do. It works offline.",
		examples: []string{
			"colligo explain count_mismatch",
			"colligo explain input_sra",
		},
	},
}

func printHelp(w io.Writer, command string, fs *flag.FlagSet) {
	text := helpTexts[command]
	var b strings.Builder
	fmt.Fprintf(&b, "usage: colligo %s [flags] %s\n\n%s\n", command, argumentsOf(command), text.does)
	if len(text.examples) > 0 {
		b.WriteString("\nexamples:\n")
		for _, e := range text.examples {
			fmt.Fprintf(&b, "  %s\n", e)
		}
	}
	b.WriteString("\n" + exitCodes + "\n\nflags:\n")
	io.WriteString(w, b.String())
	fs.SetOutput(w)
	fs.PrintDefaults()
	fs.SetOutput(io.Discard)
}

func argumentsOf(command string) string {
	if command == "explain" {
		return "<id>"
	}
	return "<path>..."
}

const unknownFlagPrefix = "flag provided but not defined: -"

func parseCommand(fs *flag.FlagSet, command string, args []string, stderr io.Writer) (rest []string, code int, finished bool) {
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	rest, err := parseInterleaved(fs, args)
	if err == nil {
		return rest, verifier.ExitOK, false
	}
	if errors.Is(err, flag.ErrHelp) {
		printHelp(stderr, command, fs)
		return nil, verifier.ExitOK, true
	}
	fmt.Fprintf(stderr, "colligo %s: %s\n", command, flagProblem(fs, command, err))
	return nil, verifier.ExitUsage, true
}

func flagProblem(fs *flag.FlagSet, command string, err error) string {
	typed, unknown := strings.CutPrefix(err.Error(), unknownFlagPrefix)
	if !unknown {
		return err.Error() + fmt.Sprintf("\nrun colligo %s -h for the flags", command)
	}
	typed = strings.TrimLeft(typed, "-")
	var known []string
	fs.VisitAll(func(f *flag.Flag) { known = append(known, f.Name) })
	slices.Sort(known)
	if near, ok := suggest.Closest(typed, known); ok {
		return fmt.Sprintf("unknown flag --%s; did you mean --%s?", typed, near)
	}
	return fmt.Sprintf("unknown flag --%s; run colligo %s -h for the flags", typed, command)
}
