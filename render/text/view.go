package text

import (
	"path/filepath"
	"slices"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/render/theme"
	"github.com/colligo-go/colligo/verifier"
)

// Options are the choices the CLI makes for a report. The renderer decides
// nothing from the environment: width, colour and verbosity arrive here.
type Options struct {
	Theme      theme.Theme
	Width      int
	Quiet      bool
	Verbose    bool
	Permissive bool
	ExitCode   int
}

type problem struct {
	observation string
	where       []string
}

type issueGroup struct {
	id       string
	mode     string
	severity model.Severity
	cause    string
	action   string
	problems []*problem
}

type runView struct {
	run    *verifier.RunReport
	files  []*verifier.FileReport
	issues []*issueGroup
}

type view struct {
	report  verifier.Report
	opts    Options
	theme   theme.Theme
	short   map[string]string
	runs    []*runView
	general []*issueGroup
}

func (r *runView) worst() model.Severity {
	var worst model.Severity
	for _, g := range r.issues {
		if g.severity.Rank() > worst.Rank() {
			worst = g.severity
		}
	}
	return worst
}

func failed(a model.Assertion) bool { return !a.Passed && !a.Skipped }

func newView(r verifier.Report, o Options) *view {
	v := &view{report: r, opts: o, theme: o.Theme, short: shortNames(r)}
	byName := map[string]*verifier.FileReport{}
	for _, f := range r.Files {
		byName[f.Name.String()] = f
	}
	owner := map[*verifier.FileReport]*runView{}
	if r.Source != nil {
		for _, run := range r.Source.Runs {
			rv := &runView{run: run, files: make([]*verifier.FileReport, len(run.Files))}
			for j, rf := range run.Files {
				if rf.Local == nil {
					continue
				}
				if f := byName[rf.Local.String()]; f != nil {
					rv.files[j], owner[f] = f, rv
				}
			}
			v.runs = append(v.runs, rv)
		}
		for _, rv := range v.runs {
			for _, a := range rv.run.Assertions {
				if failed(a) {
					rv.issues = addIssue(rv.issues, a, "")
				}
			}
		}
		for _, a := range r.Source.Assertions {
			if failed(a) {
				v.general = addIssue(v.general, a, "")
			}
		}
	}
	for _, f := range r.Files {
		rv := owner[f]
		for _, a := range f.Assertions {
			if !failed(a) {
				continue
			}
			if a.ID == diagnoses.SourceRunFailed && rv != nil && len(rv.issues) > 0 {
				continue
			}
			if rv != nil {
				rv.issues = addIssue(rv.issues, a, v.short[f.Name.String()])
			} else {
				v.general = addIssue(v.general, a, v.short[f.Name.String()])
			}
		}
	}
	bySeverity := func(a, b *issueGroup) int { return b.severity.Rank() - a.severity.Rank() }
	slices.SortStableFunc(v.general, bySeverity)
	for _, rv := range v.runs {
		slices.SortStableFunc(rv.issues, bySeverity)
	}
	return v
}

func addIssue(groups []*issueGroup, a model.Assertion, where string) []*issueGroup {
	var group *issueGroup
	for _, g := range groups {
		if g.id == a.ID && g.cause == a.Cause && g.action == a.Action {
			group = g
		}
	}
	if group == nil {
		group = &issueGroup{id: a.ID, mode: a.Mode, severity: a.Severity, cause: a.Cause, action: a.Action}
		groups = append(groups, group)
	}
	var p *problem
	for _, existing := range group.problems {
		if existing.observation == a.Observation {
			p = existing
		}
	}
	if p == nil {
		p = &problem{observation: a.Observation}
		group.problems = append(group.problems, p)
	}
	if where != "" && !slices.Contains(p.where, where) {
		p.where = append(p.where, where)
	}
	return groups
}

func shortNames(r verifier.Report) map[string]string {
	owners := map[string]map[string]bool{}
	for _, f := range r.Files {
		base := filepath.Base(f.Name.String())
		if owners[base] == nil {
			owners[base] = map[string]bool{}
		}
		owners[base][f.Name.String()] = true
	}
	short := map[string]string{}
	for _, f := range r.Files {
		full := f.Name.String()
		base := filepath.Base(full)
		if len(owners[base]) == 1 {
			short[full] = base
		} else {
			short[full] = full
		}
	}
	return short
}

func (v *view) detailRuns() []*runView {
	var out []*runView
	for _, rv := range v.runs {
		if rv.worst().Rank() > 0 {
			out = append(out, rv)
		}
	}
	slices.SortStableFunc(out, func(a, b *runView) int { return b.worst().Rank() - a.worst().Rank() })
	return out
}

func (v *view) worst() model.Severity {
	var worst model.Severity
	for _, f := range v.report.Files {
		if w := f.Worst(); w.Rank() > worst.Rank() {
			worst = w
		}
	}
	if w := v.report.Source.Worst(); w.Rank() > worst.Rank() {
		worst = w
	}
	return worst
}
