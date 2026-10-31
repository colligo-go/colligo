// Package text writes the report a person reads. It is built from blocks,
// each a function of the report, so a block can change from lines to a table,
// move, appear or go without touching the others.
package text

import (
	"io"
	"strings"

	"github.com/colligo-go/colligo/verifier"
)

type block func(v *view) []string

func Check(w io.Writer, r verifier.Report, o Options) error {
	v := newView(r, o)
	blocks := []block{verdictBlock, inputBlock}
	if !o.Quiet {
		blocks = append(blocks, summaryBlock, detailsBlock, passedBlock, checkedBlock, todoBlock, skippedBlock, footerBlock, explainBlock)
	}
	return write(w, v, blocks)
}

func write(w io.Writer, v *view, blocks []block) error {
	var parts []string
	for _, b := range blocks {
		if lines := b(v); len(lines) > 0 {
			parts = append(parts, strings.Join(lines, "\n"))
		}
	}
	_, err := io.WriteString(w, strings.Join(parts, "\n\n")+"\n")
	return err
}
