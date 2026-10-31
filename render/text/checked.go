package text

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/render/format"
	"github.com/colligo-go/colligo/render/theme"
	"github.com/colligo-go/colligo/verifier"
)

var runAccession = regexp.MustCompile(`^([EDS]RR[0-9]+)`)

var friendly = map[string]string{
	diagnoses.CountMismatch:        "base count",
	diagnoses.ReadCountUnexplained: "read count",
	diagnoses.LayoutMismatch:       "library layout",
	diagnoses.SourceStatus:         "run status",
}

type checkedLine struct {
	ok   bool
	text string
}

func checkedBlock(v *view) []string {
	checked, notChecked := v.checkedLines(), v.notCheckedLines()
	var lines []string
	if len(checked) > 0 {
		lines = append(lines, v.theme.Apply(theme.Strong, "Checked"))
		for _, c := range checked {
			mark := v.theme.Apply(theme.Good, v.theme.Verified)
			if !c.ok {
				mark = v.theme.Apply(theme.Bad, v.theme.Failed)
			}
			lines = append(lines, "  "+mark+" "+c.text)
		}
	}
	if len(notChecked) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, v.theme.Apply(theme.Strong, "Not checked"))
		for _, n := range notChecked {
			lines = append(lines, "  "+v.theme.Apply(theme.Dim, v.theme.NotChecked)+" "+n)
		}
	}
	return lines
}

func (v *view) tally(ids ...string) (evaluated, passed int) {
	for _, f := range v.report.Files {
		seen, ok := false, true
		for _, a := range f.Assertions {
			for _, id := range ids {
				if a.ID == id && !a.Skipped {
					seen, ok = true, ok && a.Passed
				}
			}
		}
		if seen {
			evaluated++
			if ok {
				passed++
			}
		}
	}
	return evaluated, passed
}

func (v *view) tallyRuns(id string) (evaluated, passed int) {
	for _, rv := range v.runs {
		for _, a := range rv.run.Assertions {
			if a.ID == id && !a.Skipped {
				evaluated++
				if a.Passed {
					passed++
				}
			}
		}
	}
	return evaluated, passed
}

func line(passed, evaluated int, singular, plural, whenAll, otherwise string) checkedLine {
	count := format.Plural(evaluated, singular, plural)
	if passed == evaluated {
		return checkedLine{true, fmt.Sprintf(whenAll, count)}
	}
	return checkedLine{false, fmt.Sprintf("%s: %d of %s pass", otherwise, passed, count)}
}

func (v *view) checkedLines() []checkedLine {
	var lines []checkedLine
	if v.report.Quick {
		return lines
	}
	if n, ok := v.tally(diagnoses.StructureInvalid); n > 0 {
		lines = append(lines, line(ok, n, "file", "files", "format and structure of %s", "format and structure"))
	}
	if n, ok := v.tally(diagnoses.ChecksumMismatch, diagnoses.SizeMismatch); n > 0 {
		whenAll, otherwise := "checksum and size of %s match the declared values", "checksum and size against the declared values"
		if v.report.Source != nil {
			whenAll = "MD5 and size of %s match what " + v.sourceLabel() + " published"
			otherwise = "MD5 and size against what " + v.sourceLabel() + " published"
		}
		lines = append(lines, line(ok, n, "file", "files", whenAll, otherwise))
	}
	if n, ok := v.tally(diagnoses.PairCount, diagnoses.PairIDs); n > 0 {
		lines = append(lines, line(ok, n, "file", "files", "the mates among %s agree in read count and read names", "the mates agree in read count and read names"))
	}
	if v.report.Source != nil {
		label := v.sourceLabel()
		if n, ok := v.tallyRuns(diagnoses.SourceStatus); n > 0 {
			lines = append(lines, line(ok, n, "run", "runs", label+" lists %s as public", label+" lists the run as public"))
		}
		if n, ok := v.tallyRuns(diagnoses.CountMismatch); n > 0 {
			lines = append(lines, line(ok, n, "run", "runs", "the bases of %s match what "+label+" declares", "bases against what "+label+" declares"))
		}
		if n, ok := v.tallyRuns(diagnoses.ReadCountUnexplained); n > 0 {
			lines = append(lines, line(ok, n, "run", "runs", "the read count of %s matches a reading seen in real runs", "read count against the readings seen in real runs"))
		}
	}
	return lines
}

func (v *view) notCheckedLines() []string {
	var lines []string
	r := v.report
	if r.Source == nil && !r.FromSamplesheet {
		lines = append(lines, "comparison with a source: not made; Colligo queries the network only when asked")
		if hint := v.sourceHint(); hint != "" {
			lines = append(lines, hint)
		}
	}
	if r.Source == nil && r.FromSamplesheet {
		lines = append(lines, "comparison with a source: not made; a samplesheet declares which files belong together, not their content")
	}
	if r.Quick {
		lines = append(lines, "the content: a quick check reads only the start, the tail and the magic bytes")
	}
	extracted := 0
	for _, rv := range v.runs {
		for j, rf := range rv.run.Files {
			if rf.Local != nil && rv.files[j] != nil && baseOf(rf.Name.String()) != baseOf(rf.Local.String()) && rf.LinkedBy == verifier.LinkedByName {
				extracted++
			}
		}
	}
	if extracted > 0 {
		lines = append(lines, fmt.Sprintf("MD5 and size of %s: not the published .gz, so they cannot match its MD5", format.Plural(extracted, "extracted file", "extracted files")))
	}
	for _, rv := range v.runs {
		for _, a := range rv.run.Assertions {
			if !a.Skipped {
				continue
			}
			what := friendly[a.ID]
			if what == "" {
				what = a.ID
			}
			switch a.SkippedBy {
			case verifier.NotDeclared:
				lines = append(lines, fmt.Sprintf("%s of run %s: %s does not declare it", what, rv.run.Accession, v.sourceLabel()))
			case verifier.NoFilesListed:
				lines = append(lines, fmt.Sprintf("%s of run %s: %s lists no file for the run", what, rv.run.Accession, v.sourceLabel()))
			default:
				lines = append(lines, fmt.Sprintf("%s of run %s: skipped, because %s failed", what, rv.run.Accession, a.SkippedBy))
			}
		}
	}
	return lines
}

func (v *view) sourceHint() string {
	var accessions []string
	seen := map[string]bool{}
	for _, f := range v.report.Files {
		if m := runAccession.FindString(baseOf(f.Name.String())); m != "" && !seen[m] {
			seen[m] = true
			accessions = append(accessions, m)
		}
	}
	switch len(accessions) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("the names look like an ENA run: add --source ena:%s to compare the files with ENA", accessions[0])
	}
	return fmt.Sprintf("the names look like %d ENA runs (%s): add --source ena:<accession> with their study to compare them with ENA", len(accessions), strings.Join(accessions[:min(3, len(accessions))], ", "))
}
