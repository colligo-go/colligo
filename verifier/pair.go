package verifier

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/model"
)

type readEnds struct {
	First string `json:"first"`
	Last  string `json:"last"`
}

func readEndsOf(r *FileReport) (readEnds, bool) {
	f, ok := r.Result.Fact("read_ends")
	if !ok {
		return readEnds{}, false
	}
	var e readEnds
	return e, json.Unmarshal(f.Value, &e) == nil
}

type mate struct {
	report  *FileReport
	records int64
	bases   int64
	ends    readEnds
}

func pairable(r *FileReport) (mate, bool) {
	if r.Content != format.ContentFastq || r.Status == StatusUnreadable || failedAtOrAbove(r.Assertions, model.Blocking) != "" {
		return mate{}, false
	}
	rec, ok := recordsOf(r.Result)
	if !ok {
		return mate{}, false
	}
	ends, ok := readEndsOf(r)
	return mate{report: r, records: rec.Records, bases: rec.Bases, ends: ends}, ok
}

func matesAgree(a, b mate) bool {
	return a.records == b.records && a.ends == b.ends
}

const (
	collapsedMateBases = 4
	collapsedMateRatio = 0.05
)

func perRead(m mate) float64 {
	return float64(m.bases) / float64(m.records)
}

// collapsedMate returns the mate that averages fewer than collapsedMateBases
// bases per read and less than collapsedMateRatio of the other mate, with the
// other one. The limits come from the measurement in RELIABILITY.md M15.
func collapsedMate(m1, m2 mate) (short, long mate, found bool) {
	if m1.records == 0 || m1.records != m2.records {
		return mate{}, mate{}, false
	}
	short, long = m1, m2
	if perRead(m2) < perRead(m1) {
		short, long = m2, m1
	}
	collapsed := perRead(short) < collapsedMateBases && perRead(short) < collapsedMateRatio*perRead(long)
	return short, long, collapsed
}

func flagCollapsedMate(m1, m2 mate) {
	short, long, found := collapsedMate(m1, m2)
	if !found {
		return
	}
	short.report.Assertions = append(short.report.Assertions, newAssertion(diagnoses.MateCollapsed, false, diagnoses.Values{
		Declared: fmt.Sprintf("%.1f bases per read in %s", perRead(long), filepath.Base(long.report.Name.String())),
		Observed: fmt.Sprintf("%.1f bases per read", perRead(short))}))
	short.report.finalize()
}

func comparePair(m1, m2 mate, where1, where2 string) []model.Assertion {
	out := []model.Assertion{newAssertion(diagnoses.PairCount, m1.records == m2.records, diagnoses.Values{
		Declared: fmt.Sprint(m1.records) + where1, Observed: fmt.Sprint(m2.records) + where2})}
	if m1.records == m2.records {
		out = append(out, newAssertion(diagnoses.PairIDs, m1.ends == m2.ends, diagnoses.Values{
			Declared: fmt.Sprintf("first=%s last=%s%s", m1.ends.First, m1.ends.Last, where1),
			Observed: fmt.Sprintf("first=%s last=%s%s", m2.ends.First, m2.ends.Last, where2)}))
	}
	return out
}

// EvaluatePairs forms pairs by file-name suffix and confirms them by the
// read_ends and records facts. A pair whose reads do not match is blocking
// on both files. A mate whose other half is not among reports gets the
// advisory pair_missing, but only when reports holds more than one file.
func EvaluatePairs(reports []*FileReport) {
	type slot [3]*FileReport
	groups := map[string]*slot{}
	var order []string
	for _, r := range reports {
		stem, n, ok := format.PairKey(r.Name)
		if !ok {
			continue
		}
		g, seen := groups[stem]
		if !seen {
			g = &slot{}
			groups[stem] = g
			order = append(order, stem)
		}
		if g[n] != nil {
			g[n] = nil
			g[0] = r
			continue
		}
		g[n] = r
	}
	for _, stem := range order {
		g := groups[stem]
		if g[0] != nil {
			continue
		}
		if (g[1] == nil) != (g[2] == nil) {
			flagMissingMate(reports, g[1], g[2])
			continue
		}
		if g[1] == nil {
			continue
		}
		m1, ok1 := pairable(g[1])
		m2, ok2 := pairable(g[2])
		if !ok1 || !ok2 {
			continue
		}
		as := comparePair(m1, m2, "", "")
		for _, r := range []*FileReport{g[1], g[2]} {
			r.Assertions = append(r.Assertions, as...)
			r.finalize()
		}
		if matesAgree(m1, m2) {
			flagCollapsedMate(m1, m2)
		}
	}
}

func flagMissingMate(reports []*FileReport, first, second *FileReport) {
	lone := first
	if lone == nil {
		lone = second
	}
	if len(reports) < 2 || lone.Status == StatusUnreadable {
		return
	}
	expected, ok := format.MateName(lone.Name)
	if !ok {
		return
	}
	lone.Assertions = append(lone.Assertions, newAssertion(diagnoses.PairMissing, false, diagnoses.Values{Declared: expected, Observed: "absent"}))
	lone.finalize()
}
