package verifier

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
)

const (
	NotDeclared   = "not_declared"
	NoFilesListed = "no_files_listed"

	statusPublic = "public"
	layoutSingle = "SINGLE"
	layoutPaired = "PAIRED"
)

// Declaration is what a source adapter returned for one accession, reduced
// to model types so that verifier never imports source/*.
type Declaration struct {
	Source    string
	Accession string
	Samples   []model.Sample
	Issues    []model.ValidationIssue
}

type SourceReport struct {
	ID         string            `json:"id"`
	Accession  string            `json:"accession"`
	Runs       []*RunReport      `json:"runs"`
	Assertions []model.Assertion `json:"assertions"`
}

type RunReport struct {
	Accession  string            `json:"accession"`
	Layout     string            `json:"layout"`
	Status     string            `json:"status"`
	Declared   model.RunCounts   `json:"declared"`
	Files      []RunFile         `json:"files"`
	Assertions []model.Assertion `json:"assertions"`
}

// RunFile is one file the source lists for a run. Local is nil when no
// checked file stands for it.
type RunFile struct {
	Name  secret.Name  `json:"name"`
	Local *secret.Name `json:"local"`
	MD5   string       `json:"md5,omitempty"`
	Size  int64        `json:"size"`
}

func (s *SourceReport) Worst() model.Severity {
	if s == nil {
		return ""
	}
	worst := worstOf(s.Assertions)
	for _, r := range s.Runs {
		if w := worstOf(r.Assertions); w.Rank() > worst.Rank() {
			worst = w
		}
	}
	return worst
}

type declaredRef struct {
	sample   int
	artifact int
}

type role int

const (
	roleLinked role = iota
	roleShadowed
	roleAmbiguous
)

type claim struct {
	ref    declaredRef
	role   role
	exact  bool
	by     int
	rivals int
}

func link(names []secret.Name, samples []model.Sample) (map[int]*claim, map[declaredRef]int, map[declaredRef]int) {
	declared := map[string]declaredRef{}
	for i, s := range samples {
		for j, a := range s.Artifacts {
			base := filepath.Base(a.Name.String())
			if _, seen := declared[base]; !seen {
				declared[base] = declaredRef{sample: i, artifact: j}
			}
		}
	}
	claims := map[int]*claim{}
	exact, copies := map[declaredRef][]int{}, map[declaredRef][]int{}
	var order []declaredRef
	for k, n := range names {
		base := filepath.Base(n.String())
		ref, isExact := declared[base]
		if !isExact {
			var ok bool
			if ref, ok = declared[base+".gz"]; !ok {
				continue
			}
		}
		if len(exact[ref])+len(copies[ref]) == 0 {
			order = append(order, ref)
		}
		claims[k] = &claim{ref: ref, exact: isExact}
		if isExact {
			exact[ref] = append(exact[ref], k)
		} else {
			copies[ref] = append(copies[ref], k)
		}
	}
	files, rivals := map[declaredRef]int{}, map[declaredRef]int{}
	for _, ref := range order {
		winners, losers := exact[ref], copies[ref]
		if len(winners) == 0 {
			winners, losers = losers, nil
		}
		if len(winners) > 1 {
			rivals[ref] = len(winners) + len(losers)
			for _, k := range append(winners, losers...) {
				claims[k].role, claims[k].rivals = roleAmbiguous, rivals[ref]
			}
			continue
		}
		files[ref] = winners[0]
		claims[winners[0]].role = roleLinked
		for _, k := range losers {
			claims[k].role, claims[k].by = roleShadowed, winners[0]
		}
	}
	return claims, files, rivals
}

// CheckAgainstSource checks the files and then compares them with what the
// source declares, following RELIABILITY.md M15 and M16: the bases of every
// file of a run are the blocking comparison, read_count is matched against
// the readings seen in real runs and is never blocking, a single label on a
// pair is advisory, and a status other than public is blocking. Each file the
// source lists and each file checked appears in the report, linked or not.
//
// A local file stands for the declared file of the same name, or of the same
// name plus .gz, because a locally extracted FASTQ is the case M15 exists for.
// Only an exact name is held to the published MD5 and size. The files of a run
// that fails get source_run_failed rather than a copy of the run assertion,
// whose observed count is not the count of any one file.
func CheckAgainstSource(ctx context.Context, names []secret.Name, decl Declaration, opts Options) (Report, error) {
	claims, files, rivals := link(names, decl.Samples)
	expected := make([]Expected, len(names))
	for k, c := range claims {
		if c.role != roleLinked || !c.exact {
			continue
		}
		a := decl.Samples[c.ref.sample].Artifacts[c.ref.artifact]
		expected[k] = Expected{MD5: a.MD5, SHA256: a.SHA256}
		if a.Size > 0 {
			size := a.Size
			expected[k].Size = &size
		}
	}
	reports, err := checkFiles(ctx, names, opts, expected)
	if err != nil {
		return Report{}, err
	}
	EvaluatePairs(reports)

	src := &SourceReport{ID: decl.Source, Accession: decl.Accession, Assertions: issueAssertions(decl.Issues), Runs: []*RunReport{}}
	where := decl.Source + " " + decl.Accession
	for k, r := range reports {
		c, claimed := claims[k]
		switch {
		case !claimed:
			r.Assertions = append(r.Assertions, newAssertion(diagnoses.SourceFileUnlisted, false, diagnoses.Values{
				Declared: where, Observed: r.Name.String()}))
		case c.role == roleShadowed:
			r.Assertions = append(r.Assertions, newAssertion(diagnoses.SourceFileUnlisted, false, diagnoses.Values{
				Declared: where, Observed: fmt.Sprintf("%s (a copy; %s stands for %s)", r.Name, reports[c.by].Name, declaredName(decl, c.ref))}))
		case c.role == roleAmbiguous:
			r.Assertions = append(r.Assertions, newAssertion(diagnoses.SourceFileAmbiguous, false, diagnoses.Values{
				Declared: declaredName(decl, c.ref), Observed: fmt.Sprint(c.rivals), Detail: decl.Samples[c.ref.sample].Accession}))
		default:
			continue
		}
		r.finalize()
	}

	for i, s := range decl.Samples {
		run := &RunReport{Accession: s.Accession, Layout: s.Layout, Status: s.SourceStatus, Declared: s.Declared, Files: []RunFile{}}
		linked := make([]*FileReport, len(s.Artifacts))
		blocker := ""
		for j, a := range s.Artifacts {
			ref := declaredRef{sample: i, artifact: j}
			rf := RunFile{Name: a.Name, MD5: a.MD5, Size: a.Size}
			if k, ok := files[ref]; ok {
				local := reports[k].Name
				rf.Local = &local
				linked[j] = reports[k]
			}
			run.Files = append(run.Files, rf)
			switch {
			case rivals[ref] > 0:
				blocker = firstOf(blocker, diagnoses.SourceFileAmbiguous)
			case linked[j] == nil:
				run.Assertions = append(run.Assertions, newAssertion(diagnoses.SourceFileMissing, false, diagnoses.Values{
					Declared: a.Name.String(), Observed: "absent", Detail: s.Accession}))
				blocker = firstOf(blocker, diagnoses.SourceFileMissing)
			default:
				blocker = firstOf(blocker, failedAtOrAbove(linked[j].Assertions, model.Blocking))
			}
		}
		run.Assertions = append([]model.Assertion{statusAssertion(s)}, run.Assertions...)
		run.Assertions = append(run.Assertions, contentAssertions(s, linked, blocker)...)
		condemn(s.Accession, run.Assertions, linked)
		src.Runs = append(src.Runs, run)
	}
	return Report{Files: reports, Source: src}, nil
}

func condemn(accession string, as []model.Assertion, files []*FileReport) {
	for _, a := range as {
		if a.Passed || a.Skipped || (a.ID != diagnoses.CountMismatch && a.ID != diagnoses.SourceStatus) {
			continue
		}
		for _, f := range files {
			if f == nil {
				continue
			}
			f.Assertions = append(f.Assertions, newAssertion(diagnoses.SourceRunFailed, false, diagnoses.Values{
				Declared: "run " + accession + " agrees with the source", Observed: a.ID, Detail: accession}))
			f.finalize()
		}
	}
}

func firstOf(current, candidate string) string {
	if current != "" {
		return current
	}
	return candidate
}

func declaredName(decl Declaration, ref declaredRef) string {
	return decl.Samples[ref.sample].Artifacts[ref.artifact].Name.String()
}

func notDeclared(id string) model.Assertion {
	a := skipped([]string{id}, NotDeclared)[0]
	a.Declared = "not declared"
	return a
}

func statusAssertion(s model.Sample) model.Assertion {
	if s.SourceStatus == "" {
		return notDeclared(diagnoses.SourceStatus)
	}
	return newAssertion(diagnoses.SourceStatus, strings.EqualFold(s.SourceStatus, statusPublic), diagnoses.Values{
		Declared: statusPublic, Observed: s.SourceStatus, Detail: s.Accession})
}

func issueAssertions(issues []model.ValidationIssue) []model.Assertion {
	out := []model.Assertion{}
	for _, issue := range issues {
		id := diagnoses.SourceFieldEmpty
		if issue.Severity == model.Blocking {
			id = diagnoses.SourceRowRejected
		}
		out = append(out, newAssertion(id, false, diagnoses.Values{
			Declared: issue.Code, Observed: issue.Message, Detail: issue.Subject}))
	}
	return out
}

var contentIDs = []string{diagnoses.CountMismatch, diagnoses.ReadCountUnexplained, diagnoses.LayoutMismatch}

type runContent struct {
	records    []int64
	bases      int64
	pairs      [][2]int
	unreadable string
}

func contentAssertions(s model.Sample, linked []*FileReport, blocker string) []model.Assertion {
	if len(s.Artifacts) == 0 {
		return skipped(contentIDs, NoFilesListed)
	}
	if blocker != "" {
		return skipped(contentIDs, blocker)
	}
	c := measure(s, linked)
	var out []model.Assertion
	baseHolds := true
	if s.Declared.BaseCount == nil {
		out = append(out, notDeclared(diagnoses.CountMismatch))
	} else {
		observed := fmt.Sprint(c.bases)
		if c.unreadable != "" {
			observed = "0 (no FASTQ records read in " + c.unreadable + ")"
		}
		baseHolds = c.unreadable == "" && c.bases == *s.Declared.BaseCount
		out = append(out, newAssertion(diagnoses.CountMismatch, baseHolds, diagnoses.Values{
			Declared: fmt.Sprint(*s.Declared.BaseCount), Observed: observed,
			Detail: fmt.Sprintf("bases over the %d file(s) of run %s", len(linked), s.Accession)}))
	}
	switch {
	case !baseHolds:
		out = append(out, skipped([]string{diagnoses.ReadCountUnexplained}, diagnoses.CountMismatch)...)
	case s.Declared.ReadCount == nil:
		out = append(out, notDeclared(diagnoses.ReadCountUnexplained))
	default:
		out = append(out, readCountAssertion(s, c))
	}
	if a, judged := layoutAssertion(s, c); judged {
		out = append(out, a)
	}
	return out
}

func measure(s model.Sample, linked []*FileReport) runContent {
	c := runContent{records: make([]int64, len(linked))}
	type slot [3]int
	stems := map[string]*slot{}
	var order []string
	for j, r := range linked {
		rec, ok := recordsOf(r.Result)
		if !ok && c.unreadable == "" {
			c.unreadable = r.Name.String()
		}
		c.records[j], c.bases = rec.Records, c.bases+rec.Bases
		stem, mate, ok := format.PairKey(s.Artifacts[j].Name)
		if !ok {
			continue
		}
		g, seen := stems[stem]
		if !seen {
			g = &slot{-1, -1, -1}
			stems[stem] = g
			order = append(order, stem)
		}
		g[mate] = j
	}
	for _, stem := range order {
		g := stems[stem]
		if g[1] < 0 || g[2] < 0 {
			continue
		}
		m1, ok1 := pairable(linked[g[1]])
		m2, ok2 := pairable(linked[g[2]])
		if ok1 && ok2 && matesAgree(m1, m2) {
			c.pairs = append(c.pairs, [2]int{g[1], g[2]})
		}
	}
	return c
}

func readCountAssertion(s model.Sample, c runContent) model.Assertion {
	want := *s.Declared.ReadCount
	each := len(c.records) > 0
	var sum int64
	for _, n := range c.records {
		each = each && n == want
		sum += n
	}
	inPair := map[int]bool{}
	var pairsPlusUnpaired int64
	for _, p := range c.pairs {
		inPair[p[0]], inPair[p[1]] = true, true
		pairsPlusUnpaired += c.records[p[0]]
	}
	for j, n := range c.records {
		if !inPair[j] {
			pairsPlusUnpaired += n
		}
	}
	declared := fmt.Sprint(want)
	values := diagnoses.Values{Declared: declared, Detail: s.Accession}
	switch {
	case each:
		values.Observed = declared + " in each file"
	case sum == want:
		values.Observed = declared + ", the sum of the files"
	case len(c.pairs) > 0 && pairsPlusUnpaired == want:
		values.Observed = declared + ", the pairs plus the unpaired files"
	default:
		values.Observed = fmt.Sprintf("per file %s, sum %d, pairs plus unpaired %d", joinCounts(c.records), sum, pairsPlusUnpaired)
		return newAssertion(diagnoses.ReadCountUnexplained, false, values)
	}
	return newAssertion(diagnoses.ReadCountUnexplained, true, values)
}

func joinCounts(counts []int64) string {
	parts := make([]string, len(counts))
	for i, n := range counts {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ", ")
}

func layoutAssertion(s model.Sample, c runContent) (model.Assertion, bool) {
	label := strings.ToUpper(s.Layout)
	paired := len(c.pairs) > 0
	observed := "no pair"
	if paired {
		p := c.pairs[0]
		observed = s.Artifacts[p[0]].Name.String() + " and " + s.Artifacts[p[1]].Name.String()
	}
	values := diagnoses.Values{Declared: label, Observed: observed, Detail: s.Accession}
	switch {
	case label == layoutSingle:
		return newAssertion(diagnoses.LayoutMismatch, !paired, values), true
	case label == layoutPaired && paired:
		return newAssertion(diagnoses.LayoutMismatch, true, values), true
	}
	return model.Assertion{}, false
}

func (s *SourceReport) writeText(w io.Writer) {
	if s == nil {
		return
	}
	fmt.Fprintf(w, "\nsource %s, accession %s: %d run(s)\n", s.ID, s.Accession, len(s.Runs))
	writeAssertions(w, s.Assertions)
	for _, r := range s.Runs {
		present := 0
		for _, f := range r.Files {
			if f.Local != nil {
				present++
			}
		}
		fmt.Fprintf(w, "run %s (%s, %s)\n  %d of %d listed file(s) present\n",
			r.Accession, orUnknown(r.Layout), orUnknown(r.Status), present, len(r.Files))
		writeAssertions(w, r.Assertions)
		for _, a := range r.Assertions {
			if a.ID == diagnoses.ReadCountUnexplained && a.Passed {
				fmt.Fprintf(w, "  read_count %s\n", a.Observed)
			}
		}
	}
}

func orUnknown(v string) string {
	if v == "" {
		return "not declared"
	}
	return v
}
