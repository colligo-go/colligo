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
	"github.com/colligo-go/colligo/verifier"
)

func runIdentify(args []string, stdout, stderr io.Writer, table identity.Table) int {
	fs := flag.NewFlagSet("identify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "write the answer as JSON")
	paths, err := parseInterleaved(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return verifier.ExitOK
		}
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
	if *jsonOut {
		if err := report.WriteJSON(stdout); err != nil {
			fmt.Fprintf(stderr, "colligo: %v\n", err)
			return verifier.ExitFatal
		}
	} else {
		report.WriteText(stdout)
	}
	return report.ExitCode()
}
