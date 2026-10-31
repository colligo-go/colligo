package text

import (
	"fmt"

	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/render/format"
	"github.com/colligo-go/colligo/render/theme"
)

func summaryBlock(v *view) []string {
	if len(v.runs) < 2 {
		return nil
	}
	dim := v.theme.Style(theme.Dim)
	rows := [][]format.Cell{{
		format.Text("Run").Styled(dim), format.Text("Layout").Styled(dim), format.Num("Files").Styled(dim),
		format.Num("Bases observed").Styled(dim), format.Num("declared").Styled(dim), format.Num("diff").Styled(dim),
		format.Text("Result").Styled(dim),
	}}
	for _, rv := range v.runs {
		rows = append(rows, v.summaryRow(rv))
	}
	return format.Table(rows, "", 2)
}

func (v *view) summaryRow(rv *runView) []format.Cell {
	present := 0
	for _, f := range rv.run.Files {
		if f.Local != nil {
			present++
		}
	}
	observed, declared, diff := "-", "not declared", ""
	if rv.run.Declared.BaseCount != nil {
		declared = format.Number(*rv.run.Declared.BaseCount)
	}
	if rv.run.Measured != nil {
		observed = format.Number(rv.run.Measured.Bases)
		if rv.run.Declared.BaseCount != nil {
			diff = format.Difference(rv.run.Measured.Bases, *rv.run.Declared.BaseCount)
		}
	}
	text, role := v.outcome(rv.worst())
	return []format.Cell{
		format.Text(rv.run.Accession), format.Text(orNone(rv.run.Layout)),
		format.Num(fmt.Sprintf("%d/%d", present, len(rv.run.Files))),
		format.Num(observed), format.Num(declared), format.Num(diff),
		format.Text(text).Styled(v.theme.Style(role)),
	}
}

func (v *view) outcome(worst model.Severity) (string, theme.Role) {
	switch {
	case worst.Rank() >= model.Blocking.Rank():
		return v.theme.Failed + " fails", theme.Bad
	case worst == model.Advisory:
		return v.theme.Advisory + " advisory", theme.Warn
	}
	return v.theme.Verified + " matches", theme.Good
}

func orNone(s string) string {
	if s == "" {
		return "not declared"
	}
	return s
}
