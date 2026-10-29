package verifier

import (
	"encoding/json"
	"fmt"

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
	return mate{report: r, records: rec.Records, ends: ends}, ok
}

// EvaluatePairs forms pairs by file-name suffix and confirms them by the
// read_ends and records facts. A pair whose suffix matches but whose reads
// do not is blocking on both files.
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
		var as []model.Assertion
		as = append(as, newAssertion(diagnoses.PairCount, m1.records == m2.records, diagnoses.Values{
			Declared: fmt.Sprint(m1.records), Observed: fmt.Sprint(m2.records)}))
		if m1.records == m2.records {
			as = append(as, newAssertion(diagnoses.PairIDs, m1.ends == m2.ends, diagnoses.Values{
				Declared: fmt.Sprintf("first=%s last=%s", m1.ends.First, m1.ends.Last),
				Observed: fmt.Sprintf("first=%s last=%s", m2.ends.First, m2.ends.Last)}))
		}
		for _, r := range []*FileReport{g[1], g[2]} {
			r.Assertions = append(r.Assertions, as...)
			r.finalize()
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
