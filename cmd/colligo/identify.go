package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/colligo-go/colligo/identity"
	jsonout "github.com/colligo-go/colligo/render/json"
	"github.com/colligo-go/colligo/render/text"
	"github.com/colligo-go/colligo/request"
	"github.com/colligo-go/colligo/verifier"
)

func runIdentify(args []string, stdout, stderr io.Writer, table identity.Table) int {
	fs := flag.NewFlagSet("identify", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "write the answer as JSON")
	var present presentation
	present.bind(fs)
	paths, exit, finished := parseCommand(fs, "identify", args, stderr)
	if finished {
		return exit
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

	arguments, err := request.NamesFromArguments(paths)
	if err != nil {
		return failed(err, stderr)
	}
	req, err := request.Resolve(ctx, request.Inputs{Paths: arguments})
	if err != nil {
		return failed(err, stderr)
	}
	counter, finish := present.startProgress(stderr)
	report, err := verifier.IdentifyAll(ctx, req.Names(), table, verifier.WithProgress(counter))
	finish()
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
