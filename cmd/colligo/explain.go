package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/render/text"
	"github.com/colligo-go/colligo/verifier"
)

func runExplain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var present presentation
	fs.StringVar(&present.color, "color", "auto", "colour the text: auto, always or never")
	ids, err := parseInterleaved(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return verifier.ExitOK
		}
		return verifier.ExitUsage
	}
	if err := present.validate(); err != nil {
		fmt.Fprintf(stderr, "colligo explain: %v\n", err)
		return verifier.ExitUsage
	}
	if len(ids) != 1 {
		fmt.Fprintf(stderr, "colligo explain: give one diagnosis id, one of:\n%s", idList(diagnoses.IDs()))
		return verifier.ExitUsage
	}
	entry, found := diagnoses.Lookup(ids[0])
	long, hasText := diagnoses.Explain(ids[0])
	if !found || !hasText {
		similar := diagnoses.Similar(ids[0], 5)
		if len(similar) == 0 {
			similar = diagnoses.IDs()
		}
		fmt.Fprintf(stderr, "colligo explain: %q is not a diagnosis id. Similar ids:\n%s", ids[0], idList(similar))
		return verifier.ExitUsage
	}
	if err := text.Explain(stdout, entry, long, present.options(stdout, verifier.ExitOK, false)); err != nil {
		fmt.Fprintf(stderr, "colligo: %v\n", err)
		return verifier.ExitFatal
	}
	return verifier.ExitOK
}

func idList(ids []string) string {
	var b strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&b, "  %s\n", id)
	}
	return b.String()
}
