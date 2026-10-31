package text

import (
	"fmt"
	"strings"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/render/format"
	"github.com/colligo-go/colligo/render/theme"
	"github.com/colligo-go/colligo/verifier"
)

type verdict struct {
	symbol   string
	word     string
	role     theme.Role
	sentence string
}

func verdictBlock(v *view) []string {
	d := v.verdict()
	head := v.theme.Apply(d.role, d.symbol+" "+d.word)
	sentence := d.sentence
	if d.role == theme.Bad {
		sentence = v.theme.Apply(theme.Strong, sentence)
	}
	return []string{fmt.Sprintf("%s  %s  %s", head, sentence, v.theme.Apply(theme.Dim, v.exitNote()))}
}

func (v *view) exitNote() string {
	if v.opts.Permissive && v.worst().Rank() >= model.Blocking.Rank() && v.opts.ExitCode == verifier.ExitOK {
		return "exit 0 (--permissive)"
	}
	return fmt.Sprintf("exit %d", v.opts.ExitCode)
}

func (v *view) verdict() verdict {
	t := v.theme
	r := v.report
	worst := v.worst()
	switch {
	case worst.Rank() >= model.Blocking.Rank():
		return verdict{t.Failed, "FAILED", theme.Bad, v.failureSentence()}
	case len(r.Files) == 0 && (r.Source == nil || len(r.Source.Runs) == 0):
		return verdict{t.Advisory, "NOTHING CHECKED", theme.Warn, "no data file was found"}
	case r.Quick:
		return verdict{t.Advisory, "QUICK", theme.Warn, format.Plural(len(r.Files), "file", "files") + " read in part: a quick check is never verified"}
	case worst == model.Advisory:
		return verdict{t.Advisory, "ADVISORY", theme.Warn, v.successSentence() + fmt.Sprintf(", with %s", format.Plural(v.advisoryCount(), "note", "notes"))}
	case v.hasGaps():
		return verdict{t.Advisory, "VERIFIED", theme.Warn, v.successSentence() + ", with gaps: see Not checked"}
	}
	return verdict{t.Verified, "VERIFIED", theme.Good, v.successSentence()}
}

func (v *view) advisoryCount() int {
	count := 0
	groups := append([]*issueGroup{}, v.general...)
	for _, rv := range v.runs {
		groups = append(groups, rv.issues...)
	}
	for _, g := range groups {
		if g.severity == model.Advisory {
			count++
		}
	}
	return count
}

func (v *view) hasGaps() bool {
	for _, rv := range v.runs {
		for _, a := range rv.run.Assertions {
			if a.Skipped && (a.SkippedBy == verifier.NotDeclared || a.SkippedBy == verifier.NoFilesListed) {
				return true
			}
		}
	}
	return false
}

func (v *view) sourceLabel() string {
	return strings.ToUpper(v.report.Source.ID)
}

func (v *view) successSentence() string {
	r := v.report
	switch {
	case r.Source != nil && len(r.Source.Runs) == 1:
		return fmt.Sprintf("%s matches %s", r.Source.Runs[0].Accession, v.sourceLabel())
	case r.Source != nil && len(r.Source.Runs) > 1:
		return fmt.Sprintf("all %d runs of %s match %s", len(r.Source.Runs), r.Source.Accession, v.sourceLabel())
	case r.FromSamplesheet:
		return "samplesheet: " + passing(len(r.Files))
	}
	return passing(len(r.Files))
}

func (v *view) failureSentence() string {
	r := v.report
	if v.onlyUnreadable() {
		n := 0
		for _, f := range r.Files {
			if f.Status == verifier.StatusUnreadable {
				n++
			}
		}
		pronoun := "them"
		if n == 1 {
			pronoun = "it"
		}
		return format.Plural(n, "file", "files") + " could not be read: nothing is verified for " + pronoun
	}
	switch {
	case r.Source != nil && len(r.Source.Runs) == 1:
		return fmt.Sprintf("%s does not match %s: do not use these files", r.Source.Runs[0].Accession, v.sourceLabel())
	case r.Source != nil && len(r.Source.Runs) > 1:
		bad := 0
		for _, rv := range v.runs {
			if rv.worst().Rank() >= model.Blocking.Rank() {
				bad++
			}
		}
		return fmt.Sprintf("%d of %d runs of %s do not match %s: do not use their files", bad, len(r.Source.Runs), r.Source.Accession, v.sourceLabel())
	case r.Source != nil:
		return fmt.Sprintf("%s does not match %s: do not use these files", r.Source.Accession, v.sourceLabel())
	}
	bad := 0
	for _, f := range r.Files {
		if f.Status == verifier.StatusInvalid || f.Status == verifier.StatusUnreadable {
			bad++
		}
	}
	prefix := ""
	if r.FromSamplesheet {
		prefix = "samplesheet: "
	}
	return fmt.Sprintf("%s%d of %s failed a check", prefix, bad, format.Plural(len(r.Files), "file", "files"))
}

func (v *view) onlyUnreadable() bool {
	seen := false
	groups := append([]*issueGroup{}, v.general...)
	for _, rv := range v.runs {
		groups = append(groups, rv.issues...)
	}
	for _, g := range groups {
		if g.severity.Rank() < model.Blocking.Rank() {
			continue
		}
		if g.id != diagnoses.ReadFailed {
			return false
		}
		seen = true
	}
	return seen && v.report.Source == nil
}

func passing(files int) string {
	if files == 1 {
		return "1 file passes every check run"
	}
	return fmt.Sprintf("%s pass every check run", format.Plural(files, "file", "files"))
}
