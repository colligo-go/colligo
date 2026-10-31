package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/colligo-go/colligo/identity"
	jsonout "github.com/colligo-go/colligo/render/json"
	"github.com/colligo-go/colligo/render/text"
	"github.com/colligo-go/colligo/verifier"
)

func runIdentify(args []string, stdout, stderr io.Writer, table identity.Table) int {
	fs := flag.NewFlagSet("identify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "write the answer as JSON")
	var present presentation
	present.bind(fs)
	paths, err := parseInterleaved(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return verifier.ExitOK
		}
		return verifier.ExitUsage
	}
	if err := present.validate(); err != nil {
		fmt.Fprintf(stderr, "colligo identify: %v\n", err)
		return verifier.ExitUsage
	}
	if len(paths) == 0 {
		fmt.Fprintln(stderr, "colligo identify: no path given")
		return verifier.ExitUsage
	}
	if err := identity.Verify(); err != nil {
		fmt.Fprintf(stderr, "colligo: %v\nthe tool or its environment is suspect; nothing was identified\n", err)
		return verifier.ExitFatal
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	names, _, _, err := collect(ctx, paths, false)
	if err != nil {
		return failed(err, stderr)
	}
	report, err := verifier.IdentifyAll(ctx, names, table)
	if err != nil {
		return failed(err, stderr)
	}
	code := report.ExitCode()
	if *jsonOut {
		err = jsonout.Identify(stdout, report)
	} else {
		err = text.Identify(stdout, report, present.options(stdout, code, false))
	}
	if err != nil {
		fmt.Fprintf(stderr, "colligo: %v\n", err)
		return verifier.ExitFatal
	}
	return code
}
