package text

import (
	"fmt"
	"strings"

	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/render/format"
	"github.com/colligo-go/colligo/render/theme"
	"github.com/colligo-go/colligo/verifier"
)

const labelWidth = 7

func detailsBlock(v *view) []string {
	var sections [][]string
	for _, rv := range v.detailRuns() {
		sections = append(sections, v.runSection(rv))
	}
	if general := v.generalSection(); len(general) > 0 {
		sections = append(sections, general)
	}
	var lines []string
	for i, s := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, s...)
	}
	return lines
}

func (v *view) runSection(rv *runView) []string {
	title := fmt.Sprintf("Run %s", rv.run.Accession)
	facts := []string{orNone(rv.run.Layout)}
	if rv.run.Status != "" {
		facts = append(facts, v.sourceLabel()+" status "+rv.run.Status)
	}
	lines := []string{v.theme.Apply(theme.Strong, title) + v.theme.Apply(theme.Dim, "  "+strings.Join(facts, " · "))}
	if numbers := v.runNumbers(rv); numbers != "" {
		lines = append(lines, v.labelled("  ", "Bases", numbers)...)
	}
	for _, g := range rv.issues {
		lines = append(lines, v.issueLines(g, "  ")...)
	}
	return append(lines, v.runFileTable(rv, "  ")...)
}

func (v *view) runNumbers(rv *runView) string {
	declared, measured := rv.run.Declared.BaseCount, rv.run.Measured
	if declared == nil || measured == nil {
		return ""
	}
	return fmt.Sprintf("%s declared by %s, %s observed (%s)", format.Number(*declared), v.sourceLabel(), format.Number(measured.Bases), format.Difference(measured.Bases, *declared))
}

func (v *view) generalSection() []string {
	var lines []string
	for _, g := range v.general {
		lines = append(lines, v.issueLines(g, "")...)
	}
	if len(lines) == 0 && v.report.Source != nil {
		return nil
	}
	if table := v.generalFileTable(); len(table) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, table...)
	}
	return lines
}

func (v *view) issueLines(g *issueGroup, indent string) []string {
	symbol, role := v.theme.Failed, theme.Bad
	if g.severity == model.Advisory {
		symbol, role = v.theme.Advisory, theme.Warn
	}
	head := v.theme.Apply(role, symbol+" "+g.id) + v.theme.Apply(theme.Dim, fmt.Sprintf(" · %s · %s", g.severity, g.mode))
	lines := []string{indent + head}
	for _, p := range g.problems {
		lines = append(lines, v.labelled(indent+"  ", "Problem", p.observation)...)
		if len(p.where) > 0 {
			lines = append(lines, v.labelledAs(indent+"  ", "", "in "+whereText(p.where), theme.Dim)...)
		}
	}
	lines = append(lines, v.labelled(indent+"  ", "Likely", "(hypothesis) "+g.cause)...)
	return append(lines, v.labelled(indent+"  ", "Do", g.action)...)
}

func whereText(where []string) string {
	if len(where) > 4 {
		return fmt.Sprintf("%s and %d more", strings.Join(where[:4], ", "), len(where)-4)
	}
	return strings.Join(where, ", ")
}

func (v *view) labelled(indent, label, text string) []string {
	return v.labelledAs(indent, label, text, theme.Plain)
}

func (v *view) labelledAs(indent, label, text string, role theme.Role) []string {
	plain := indent + padRight(label, labelWidth) + "  "
	width := 0
	if v.opts.Width > 0 {
		width = max(v.opts.Width-format.Width(plain), 20)
	}
	wrapped := format.Wrap(text, width)
	lines := make([]string, len(wrapped))
	for i, w := range wrapped {
		if i == 0 && label != "" {
			lines[i] = indent + v.theme.Apply(theme.Dim, padRight(label, labelWidth)) + "  " + v.theme.Apply(role, w)
			continue
		}
		lines[i] = strings.Repeat(" ", format.Width(plain)) + v.theme.Apply(role, w)
	}
	return lines
}

func padRight(s string, width int) string {
	if gap := width - format.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

type fileRow struct {
	name    string
	note    string
	report  *verifier.FileReport
	missing bool
}

func (v *view) runFileTable(rv *runView, indent string) []string {
	var rows []fileRow
	for j, rf := range rv.run.Files {
		f := rv.files[j]
		if f == nil {
			rows = append(rows, fileRow{name: baseOf(rf.Name.String()), missing: true})
			continue
		}
		row := fileRow{name: v.short[f.Name.String()], report: f}
		declared, local := baseOf(rf.Name.String()), baseOf(f.Name.String())
		if declared != local && declared != local+".gz" {
			row.note = "for " + declared
		}
		rows = append(rows, row)
	}
	return v.fileTable(rows, indent)
}

func (v *view) generalFileTable() []string {
	inRun := map[*verifier.FileReport]bool{}
	for _, rv := range v.runs {
		for _, f := range rv.files {
			inRun[f] = true
		}
	}
	var rows []fileRow
	for _, f := range v.report.Files {
		if !inRun[f] {
			rows = append(rows, fileRow{name: v.short[f.Name.String()], report: f})
		}
	}
	return v.fileTable(rows, "")
}

func (v *view) fileTable(rows []fileRow, indent string) []string {
	if len(rows) == 0 {
		return nil
	}
	dim := v.theme.Style(theme.Dim)
	table := [][]format.Cell{{
		format.Text("File").Styled(dim), format.Num("Reads").Styled(dim), format.Num("Bases").Styled(dim),
		format.Num("Bases/read").Styled(dim), format.Text("Result").Styled(dim),
	}}
	for _, r := range rows {
		table = append(table, v.fileCells(r))
	}
	return format.Table(table, indent, 2)
}

func (v *view) fileCells(r fileRow) []format.Cell {
	name := r.name
	if r.note != "" {
		name += "  (" + r.note + ")"
	}
	nameCell := format.Text(name)
	if r.missing {
		return []format.Cell{nameCell, format.Num("-"), format.Num("-"), format.Num("-"),
			format.Text(v.theme.Failed + " missing").Styled(v.theme.Style(theme.Bad))}
	}
	reads, bases, perRead := "-", "-", "-"
	if n, b, ok := r.report.Records(); ok {
		reads, bases, perRead = format.Number(n), format.Number(b), format.PerRead(b, n)
	}
	text, role := v.result(r.report)
	return []format.Cell{nameCell, format.Num(reads), format.Num(bases), format.Num(perRead), format.Text(text).Styled(v.theme.Style(role))}
}
