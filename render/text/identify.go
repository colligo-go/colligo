package text

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/colligo-go/colligo/render/format"
	"github.com/colligo-go/colligo/render/theme"
	"github.com/colligo-go/colligo/verifier"
)

type identifyView struct {
	report verifier.IdentifyReport
	opts   Options
	theme  theme.Theme
}

type identifyBlock func(v *identifyView) []string

func Identify(w io.Writer, r verifier.IdentifyReport, o Options) error {
	v := &identifyView{report: r, opts: o, theme: o.Theme}
	blocks := []identifyBlock{identifyVerdict}
	if !o.Quiet {
		blocks = append(blocks, identifyAnswers, identifyChecked, identifyFooter)
	}
	var parts []string
	for _, b := range blocks {
		if lines := b(v); len(lines) > 0 {
			parts = append(parts, strings.Join(lines, "\n"))
		}
	}
	_, err := io.WriteString(w, strings.Join(parts, "\n\n")+"\n")
	return err
}

func (v *identifyView) counts() map[string]int {
	counts := map[string]int{}
	for _, f := range v.report.Files {
		counts[f.Status]++
	}
	return counts
}

func identifyVerdict(v *identifyView) []string {
	counts := v.counts()
	t := v.theme
	var head, sentence string
	role := theme.Good
	switch {
	case len(v.report.Files) == 0:
		head, role, sentence = t.Failed+" NOTHING IDENTIFIED", theme.Bad, "no file was found, so nothing is identified"
	case counts[verifier.StatusUnreadable] > 0:
		head, role = t.Failed+" FAILED", theme.Bad
		sentence = format.Plural(counts[verifier.StatusUnreadable], "file", "files") + " could not be read: nothing is identified for them"
	case counts[verifier.StatusInvalid]+counts[verifier.IdentityConflict] > 0:
		head, role = t.Failed+" FAILED", theme.Bad
		sentence = v.contradiction()
	case counts[verifier.IdentityNotIdentified]+counts[verifier.IdentityDeclared] > 0:
		head, role = t.Advisory+" NOT CONFIRMED", theme.Warn
		sentence = v.unconfirmed()
	default:
		head = t.Verified + " IDENTIFIED"
		sentence = v.identified()
	}
	sentence = strings.TrimSpace(sentence)
	if role == theme.Bad {
		sentence = t.Apply(theme.Strong, sentence)
	}
	return []string{fmt.Sprintf("%s  %s  %s", t.Apply(role, head), sentence, t.Apply(theme.Dim, fmt.Sprintf("exit %d", v.opts.ExitCode)))}
}

func (v *identifyView) only() *verifier.Identification {
	if len(v.report.Files) == 1 {
		return v.report.Files[0]
	}
	return nil
}

func (v *identifyView) contradiction() string {
	if f := v.only(); f != nil {
		if f.Status == verifier.IdentityConflict {
			return fmt.Sprintf("%s says it is %s, but its content says otherwise", filepath.Base(f.Name.String()), f.Gencode.Label)
		}
		return fmt.Sprintf("%s is not a valid %s file", filepath.Base(f.Name.String()), f.Content)
	}
	return v.summary()
}

func (v *identifyView) unconfirmed() string {
	if f := v.only(); f != nil {
		name := filepath.Base(f.Name.String())
		if f.Status == verifier.IdentityDeclared {
			return fmt.Sprintf("%s says it is %s, and nothing official confirms it", name, f.Gencode.Label)
		}
		return fmt.Sprintf("%s matches no known reference", name)
	}
	return v.summary()
}

func (v *identifyView) identified() string {
	if f := v.only(); f != nil {
		return fmt.Sprintf("%s is %s", filepath.Base(f.Name.String()), identifiedLabel(f))
	}
	return format.Plural(len(v.report.Files), "file", "files") + " identified"
}

func identifiedLabel(f *verifier.Identification) string {
	switch {
	case f.Checksum != nil:
		return f.Checksum.Label
	case f.Reference != nil:
		return f.Reference.Label
	}
	return "identified"
}

func (v *identifyView) summary() string {
	counts := v.counts()
	var parts []string
	for _, status := range []string{verifier.IdentityIdentified, verifier.IdentityDeclared, verifier.IdentityNotIdentified, verifier.IdentityConflict, verifier.StatusInvalid, verifier.StatusUnreadable} {
		if counts[status] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[status], strings.ReplaceAll(status, "_", " ")))
		}
	}
	return format.Plural(len(v.report.Files), "file", "files") + ": " + strings.Join(parts, ", ")
}

func identifyAnswers(v *identifyView) []string {
	var lines []string
	for i, f := range v.report.Files {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, v.answer(f)...)
	}
	return lines
}

func (v *identifyView) answer(f *verifier.Identification) []string {
	t := v.theme
	symbol, role := t.Advisory, theme.Warn
	switch f.Status {
	case verifier.IdentityIdentified:
		symbol, role = t.Verified, theme.Good
	case verifier.IdentityConflict, verifier.StatusInvalid, verifier.StatusUnreadable:
		symbol, role = t.Failed, theme.Bad
	}
	name := filepath.Base(f.Name.String())
	lines := []string{t.Apply(role, symbol+" "+name) + t.Apply(theme.Dim, "  "+strings.ReplaceAll(f.Status, "_", " "))}
	type pair struct{ field, value string }
	var rows []pair
	add := func(field, value string) { rows = append(rows, pair{field, value}) }
	switch f.Status {
	case verifier.IdentityIdentified:
		if f.Checksum != nil {
			add("identified", f.Checksum.Label+" ("+f.Checksum.Asset+")")
			add("md5", f.Checksum.MD5)
			add("source", f.Checksum.Origin+", read on "+f.Checksum.Retrieved)
		}
		if f.Reference != nil {
			if f.Checksum == nil {
				add("identified", f.Reference.Label)
			}
			add("seqcol", fmt.Sprintf("%s (%d sequences)", f.Seqcol.Digest, f.Seqcol.Sequences))
			add("source", f.Reference.Source)
		}
	case verifier.IdentityDeclared, verifier.IdentityConflict:
		add("header says", f.Gencode.Label)
		add("release", f.Gencode.Release)
		add("organism", f.Gencode.Organism)
		if f.Gencode.Assembly != "" {
			add("assembly", f.Gencode.Assembly)
		}
		if f.Gencode.Ensembl != "" {
			add("ensembl", f.Gencode.Ensembl)
		}
	case verifier.IdentityNotIdentified:
		if f.Seqcol != nil {
			add("seqcol", fmt.Sprintf("%s (%d sequences)", f.Seqcol.Digest, f.Seqcol.Sequences))
		}
	}
	reason := "note"
	if f.Status == verifier.IdentityConflict || f.Status == verifier.StatusInvalid || f.Status == verifier.StatusUnreadable {
		reason = "problem"
	}
	keyWidth := format.Width(reason)
	for _, r := range rows {
		keyWidth = max(keyWidth, format.Width(r.field))
	}
	for _, r := range rows {
		lines = append(lines, "  "+t.Apply(theme.Dim, padRight(r.field, keyWidth))+"  "+r.value)
	}
	if f.Reason != "" {
		lines = append(lines, labelledText(t, v.opts.Width, "  ", reason, keyWidth, f.Reason)...)
	}
	return lines
}

func labelledText(t theme.Theme, width int, indent, label string, labelW int, text string) []string {
	prefix := indent + padRight(label, labelW) + "  "
	wrapAt := 0
	if width > 0 {
		wrapAt = max(width-format.Width(prefix), 20)
	}
	wrapped := format.Wrap(text, wrapAt)
	lines := make([]string, len(wrapped))
	for i, w := range wrapped {
		if i == 0 {
			lines[i] = indent + t.Apply(theme.Dim, padRight(label, labelW)) + "  " + w
			continue
		}
		lines[i] = strings.Repeat(" ", format.Width(prefix)) + w
	}
	return lines
}

func identifyChecked(v *identifyView) []string {
	var checked, not []string
	readable, fasta, declared := 0, 0, 0
	for _, f := range v.report.Files {
		switch f.Status {
		case verifier.StatusUnreadable, verifier.StatusInvalid:
			continue
		}
		readable++
		if f.Seqcol != nil {
			fasta++
		}
		if f.Status == verifier.IdentityDeclared {
			declared++
		}
	}
	if readable > 0 {
		checked = append(checked, "MD5 of "+format.Plural(readable, "file", "files")+" against the official checksums Colligo carries")
	}
	if fasta > 0 {
		checked = append(checked, "sequence collection of "+format.Plural(fasta, "FASTA", "FASTAs")+" against the known references")
	}
	if declared > 0 {
		not = append(not, "that the header of "+format.Plural(declared, "GTF", "GTFs")+" is true: only a matching official checksum proves it")
	}
	var lines []string
	if len(checked) > 0 {
		lines = append(lines, v.theme.Apply(theme.Strong, "Checked"))
		for _, c := range checked {
			lines = append(lines, bullet(v.theme, v.opts.Width, v.theme.Apply(theme.Good, v.theme.Verified), c)...)
		}
	}
	if len(not) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, v.theme.Apply(theme.Strong, "Not checked"))
		for _, n := range not {
			lines = append(lines, bullet(v.theme, v.opts.Width, v.theme.Apply(theme.Dim, v.theme.NotChecked), n)...)
		}
	}
	return lines
}

func identifyFooter(v *identifyView) []string {
	var size int64
	for _, f := range v.report.Files {
		size += f.Size
	}
	parts := []string{v.summary(), format.Size(size)}
	if v.report.Elapsed > 0 {
		parts = append(parts, format.Duration(v.report.Elapsed))
	}
	return dimmedText(v.theme, v.opts.Width, strings.Join(parts, " · "))
}

func dimmedText(t theme.Theme, width int, text string) []string {
	wrapped := format.Wrap(text, width)
	for i, w := range wrapped {
		wrapped[i] = t.Apply(theme.Dim, w)
	}
	return wrapped
}

func bullet(t theme.Theme, width int, mark, text string) []string {
	wrapAt := 0
	if width > 0 {
		wrapAt = max(width-4, 20)
	}
	wrapped := format.Wrap(text, wrapAt)
	lines := make([]string, len(wrapped))
	for i, w := range wrapped {
		if i == 0 {
			lines[i] = "  " + mark + " " + w
			continue
		}
		lines[i] = "    " + w
	}
	return lines
}
