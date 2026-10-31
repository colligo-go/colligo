package verifier

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/internal/fsaccess"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/samplesheet"
	"github.com/colligo-go/colligo/secret"
)

const (
	firstMate  = 1
	secondMate = 2
	rootDigits = 12
)

type sheetRow struct {
	row     samplesheet.Row
	reports [3]*FileReport
}

type sheetRun struct {
	rows    []sheetRow
	touched []*FileReport
	marked  map[*FileReport]bool
}

func (s *sheetRun) attach(a model.Assertion, reports ...*FileReport) {
	for i, r := range reports {
		if slices.Contains(reports[:i], r) {
			continue
		}
		r.Assertions = append(r.Assertions, a)
		if !s.marked[r] {
			s.marked[r] = true
			s.touched = append(s.touched, r)
		}
	}
}

// CheckSamplesheet checks every file a samplesheet lists, each one read
// once however many lines name it, and then the claims the samplesheet makes
// about them: which files are mates and which lines are the same data. Mates
// are the ones the samplesheet declares, not the ones the names suggest. A
// remote location is blocking, because nothing about it is checked.
func CheckSamplesheet(ctx context.Context, sheet samplesheet.Sheet, opts Options) (Report, error) {
	names := localNames(sheet)
	reports, err := checkFiles(ctx, names, opts, nil)
	if err != nil {
		return Report{}, err
	}
	run := &sheetRun{marked: map[*FileReport]bool{}}
	var files []*FileReport
	listed := map[*FileReport]bool{}
	for _, row := range sheet.Rows {
		entry := sheetRow{row: row}
		for slot, ref := range [3]samplesheet.Reference{firstMate: row.Fastq1, secondMate: row.Fastq2} {
			switch {
			case !ref.Set():
			case ref.Remote():
				entry.reports[slot] = remoteReport(row, ref)
			default:
				entry.reports[slot] = reports[indexOf(names, ref.Name())]
			}
		}
		for slot := firstMate; slot <= secondMate; slot++ {
			if r := entry.reports[slot]; r != nil && !listed[r] {
				listed[r] = true
				files = append(files, r)
			}
		}
		run.rows = append(run.rows, entry)
	}
	run.annotateUnreadable()
	run.evaluateMates()
	run.evaluateRepetition()
	for _, r := range run.touched {
		r.finalize()
	}
	return Report{Files: files}, nil
}

func indexOf(names []secret.Name, name secret.Name) int {
	return slices.IndexFunc(names, func(candidate secret.Name) bool {
		return fsaccess.SamePath(candidate.Raw, name.Raw)
	})
}

func localNames(sheet samplesheet.Sheet) []secret.Name {
	var names []secret.Name
	for _, row := range sheet.Rows {
		for _, ref := range []samplesheet.Reference{row.Fastq1, row.Fastq2} {
			if ref.Set() && !ref.Remote() && indexOf(names, ref.Name()) < 0 {
				names = append(names, ref.Name())
			}
		}
	}
	return names
}

func remoteReport(row samplesheet.Row, ref samplesheet.Reference) *FileReport {
	rep := newReport(secret.NewName(ref.Endpoint().String()), StatusInvalid)
	rep.Assertions = []model.Assertion{newAssertion(diagnoses.SheetRemotePath, false, diagnoses.Values{
		Declared: "a local file", Observed: ref.Endpoint().String(), Detail: fmt.Sprint(row.Line)})}
	rep.finalize()
	return rep
}

func (s *sheetRun) linesOf(report *FileReport) []int {
	var lines []int
	for _, e := range s.rows {
		for slot := firstMate; slot <= secondMate; slot++ {
			if e.reports[slot] == report && !slices.Contains(lines, e.row.Line) {
				lines = append(lines, e.row.Line)
			}
		}
	}
	return lines
}

func (s *sheetRun) annotateUnreadable() {
	seen := map[*FileReport]bool{}
	for _, e := range s.rows {
		for slot := firstMate; slot <= secondMate; slot++ {
			r := e.reports[slot]
			if r == nil || r.Status != StatusUnreadable || seen[r] {
				continue
			}
			seen[r] = true
			where := "samplesheet " + describeLines(s.linesOf(r))
			for i := range r.Assertions {
				a := &r.Assertions[i]
				if a.ID == diagnoses.ReadFailed && !a.Passed {
					a.Declared = where
					a.Observation = strings.TrimSuffix(a.Observation, ".") + " (listed in " + where + ")."
				}
			}
		}
	}
}

type declaredPair struct {
	entry int
	first mate
	other mate
}

func (s *sheetRun) evaluateMates() {
	var disagreeing []declaredPair
	for i, e := range s.rows {
		r1, r2 := e.reports[firstMate], e.reports[secondMate]
		if r1 == nil || r2 == nil {
			continue
		}
		m1, ok1 := pairable(r1)
		m2, ok2 := pairable(r2)
		if !ok1 || !ok2 {
			continue
		}
		line := e.row.Line
		for _, a := range comparePair(m1, m2, fmt.Sprintf(" (fastq_1, line %d)", line), fmt.Sprintf(" (fastq_2, line %d)", line)) {
			s.attach(a, r1, r2)
		}
		if !matesAgree(m1, m2) {
			disagreeing = append(disagreeing, declaredPair{entry: i, first: m1, other: m2})
		}
	}
	for i, a := range disagreeing {
		for _, b := range disagreeing[i+1:] {
			if !matesAgree(a.first, b.other) || !matesAgree(b.first, a.other) {
				continue
			}
			lineA, lineB := s.rows[a.entry].row.Line, s.rows[b.entry].row.Line
			s.attach(newAssertion(diagnoses.SheetMatesSwapped, false, diagnoses.Values{
				Declared: fmt.Sprint(lineA), Observed: fmt.Sprint(lineB)}),
				a.first.report, a.other.report, b.first.report, b.other.report)
		}
	}
}

type position struct {
	entry int
	slot  int
}

func (s *sheetRun) rootAt(p position) string {
	if r := s.rows[p.entry].reports[p.slot]; r != nil {
		return r.BLAKE3
	}
	return ""
}

func (s *sheetRun) sameFiles(a, b int) bool {
	for slot := firstMate; slot <= secondMate; slot++ {
		ra, rb := s.rows[a].reports[slot], s.rows[b].reports[slot]
		if (ra == nil) != (rb == nil) {
			return false
		}
		if ra != nil && (ra.BLAKE3 == "" || ra.BLAKE3 != rb.BLAKE3) {
			return false
		}
	}
	return true
}

func (s *sheetRun) evaluateRepetition() {
	byRoot := map[string][]position{}
	var order []string
	for i := range s.rows {
		for slot := firstMate; slot <= secondMate; slot++ {
			p := position{entry: i, slot: slot}
			root := s.rootAt(p)
			if root == "" {
				continue
			}
			if _, seen := byRoot[root]; !seen {
				order = append(order, root)
			}
			byRoot[root] = append(byRoot[root], p)
		}
	}
	repeated := map[[2]int]bool{}
	for _, root := range order {
		positions := byRoot[root]
		for i, a := range positions {
			for _, b := range positions[i+1:] {
				s.compare(root, a, b, repeated)
			}
		}
	}
}

func (s *sheetRun) compare(root string, a, b position, repeated map[[2]int]bool) {
	ea, eb := s.rows[a.entry], s.rows[b.entry]
	reports := []*FileReport{ea.reports[a.slot], eb.reports[b.slot]}
	short := root[:min(len(root), rootDigits)]
	switch {
	case a.entry == b.entry:
		s.attach(newAssertion(diagnoses.SheetSharedFile, false, diagnoses.Values{
			Declared: short, Detail: fmt.Sprintf("line %d (fastq_1 and fastq_2)", ea.row.Line)}), reports...)
	case ea.row.Sample == eb.row.Sample && s.sameFiles(a.entry, b.entry):
		key := [2]int{a.entry, b.entry}
		if repeated[key] {
			return
		}
		repeated[key] = true
		s.attach(newAssertion(diagnoses.SheetDuplicateRow, false, diagnoses.Values{
			Declared: fmt.Sprintf("%d and %d", ea.row.Line, eb.row.Line), Detail: ea.row.Sample}), reports...)
	default:
		s.attach(newAssertion(diagnoses.SheetSharedFile, false, diagnoses.Values{
			Declared: short, Detail: describeLines([]int{ea.row.Line, eb.row.Line})}), reports...)
	}
}

func describeLines(lines []int) string {
	slices.Sort(lines)
	parts := make([]string, len(lines))
	for i, l := range lines {
		parts[i] = fmt.Sprint(l)
	}
	switch len(parts) {
	case 1:
		return "line " + parts[0]
	case 2:
		return "lines " + parts[0] + " and " + parts[1]
	}
	return "lines " + strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}
