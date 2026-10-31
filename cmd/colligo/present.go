package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/colligo-go/colligo/render/format"
	"github.com/colligo-go/colligo/render/progress"
	"github.com/colligo-go/colligo/render/text"
	"github.com/colligo-go/colligo/render/theme"
	"github.com/colligo-go/colligo/verifier"
)

type presentation struct {
	quiet   bool
	verbose bool
	color   string
}

func (p *presentation) bind(fs *flag.FlagSet) {
	fs.BoolVar(&p.quiet, "quiet", false, "print only the verdict line")
	fs.BoolVar(&p.verbose, "verbose", false, "also list skipped files and every assertion that passed")
	fs.StringVar(&p.color, "color", "auto", "colour the text report: auto, always or never")
}

func (p *presentation) validate() error {
	if _, ok := theme.ParseMode(p.color); !ok {
		return fmt.Errorf("--color takes auto, always or never, not %q", p.color)
	}
	if p.quiet && p.verbose {
		return fmt.Errorf("--quiet and --verbose cannot be combined")
	}
	return nil
}

func (p *presentation) startProgress(stderr io.Writer) (*verifier.Progress, func()) {
	isTerminal, _ := format.Terminal(stderr)
	if !isTerminal || p.quiet {
		return nil, func() {}
	}
	mode, _ := theme.ParseMode(p.color)
	bar := progress.New(stderr, theme.Choose(mode, true, os.Getenv("NO_COLOR") != ""))
	return verifier.NewProgress(bar.Update), bar.Finish
}

func (p *presentation) options(stdout io.Writer, exitCode int, permissive bool) text.Options {
	mode, _ := theme.ParseMode(p.color)
	isTerminal, width := format.Terminal(stdout)
	return text.Options{
		Theme:      theme.Choose(mode, isTerminal, os.Getenv("NO_COLOR") != ""),
		Width:      width,
		Quiet:      p.quiet,
		Verbose:    p.verbose,
		Permissive: permissive,
		ExitCode:   exitCode,
	}
}
