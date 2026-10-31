// Package verifier evaluates assertions over facts, with the defence
// precedence of RELIABILITY.md §5, and renders the result. It never touches
// a database: it is the light door.
package verifier

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/facts"
	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
)

type Expected struct {
	MD5    string
	SHA256 string
	BLAKE3 string
	Size   *int64
	Reads  *int64
	Bases  *int64
}

func AssertionIDs() []string {
	return []string{
		diagnoses.ReadFailed, diagnoses.ChangedDuringRead, diagnoses.HTMLBody, diagnoses.MagicMismatch,
		diagnoses.ChecksumMismatch, diagnoses.SizeMismatch, diagnoses.StructureInvalid,
		diagnoses.FormatUnknown, diagnoses.FormatDeclared, diagnoses.CountMismatch,
		diagnoses.PairCount, diagnoses.PairIDs, diagnoses.PairMissing, diagnoses.ExtensionMismatch, diagnoses.TrailingBlankLines,
		diagnoses.SheetRemotePath, diagnoses.SheetSharedFile, diagnoses.SheetDuplicateRow, diagnoses.SheetMatesSwapped,
		diagnoses.SourceStatus, diagnoses.SourceFileMissing, diagnoses.SourceFileUnlisted, diagnoses.SourceFileAmbiguous,
		diagnoses.ReadCountUnexplained, diagnoses.LayoutMismatch, diagnoses.SourceRowRejected, diagnoses.SourceFieldEmpty, diagnoses.SourceRunFailed,
	}
}

func newAssertion(id string, passed bool, v diagnoses.Values) model.Assertion {
	entry, ok := diagnoses.Lookup(id)
	if !ok {
		panic("assertion without catalogue entry: " + id)
	}
	a := model.Assertion{ID: id, Mode: entry.Mode, Severity: entry.Severity, Passed: passed, Declared: v.Declared, Observed: v.Observed}
	if !passed {
		a.Observation, a.Cause, a.Action = entry.Describe(v)
	}
	return a
}

func skipped(ids []string, by string) []model.Assertion {
	out := make([]model.Assertion, 0, len(ids))
	for _, id := range ids {
		entry, _ := diagnoses.Lookup(id)
		out = append(out, model.Assertion{ID: id, Mode: entry.Mode, Severity: entry.Severity, Skipped: true, SkippedBy: by})
	}
	return out
}

func failedAtOrAbove(as []model.Assertion, floor model.Severity) string {
	for _, a := range as {
		if !a.Passed && !a.Skipped && a.Severity.Rank() >= floor.Rank() {
			return a.ID
		}
	}
	return ""
}

type stage struct {
	ids []string
	run func() []model.Assertion
}

// Evaluate computes the assertions of one file, in precedence order. A
// failure at a higher level makes the checks below it meaningless, so they
// are recorded as skipped instead of piling up derived failures.
func Evaluate(name secret.Name, res facts.Result, exp Expected, declared string, withHashes bool) []model.Assertion {
	wantContainer, wantContent := format.FromName(name)
	if format.Known(declared) {
		if v, _ := format.Lookup(declared); v.Compressed() {
			wantContainer = declared
		} else {
			wantContent = declared
		}
	}
	stages := []stage{
		{ids: []string{diagnoses.HTMLBody, diagnoses.MagicMismatch}, run: func() []model.Assertion { return magicStage(res, wantContainer) }},
		{ids: []string{diagnoses.ChecksumMismatch, diagnoses.SizeMismatch}, run: func() []model.Assertion { return integrityStage(res, exp, withHashes) }},
		{ids: []string{diagnoses.StructureInvalid, diagnoses.TrailingBlankLines}, run: func() []model.Assertion { return structureStage(res) }},
		{ids: []string{diagnoses.FormatUnknown, diagnoses.FormatDeclared}, run: func() []model.Assertion { return formatStage(res, declared) }},
		{ids: []string{diagnoses.CountMismatch}, run: func() []model.Assertion { return countStage(res, exp) }},
		{ids: []string{diagnoses.ExtensionMismatch}, run: func() []model.Assertion { return extensionStage(res, wantContainer, wantContent) }},
	}
	var out []model.Assertion
	for _, st := range stages {
		if by := failedAtOrAbove(out, model.Blocking); by != "" {
			out = append(out, skipped(st.ids, by)...)
			continue
		}
		out = append(out, st.run()...)
	}
	return out
}

func magicStage(res facts.Result, wantContainer string) []model.Assertion {
	if res.Content == format.ContentHTML {
		return []model.Assertion{newAssertion(diagnoses.HTMLBody, false, diagnoses.Values{Declared: wantContainer, Observed: "html"})}
	}
	out := []model.Assertion{newAssertion(diagnoses.HTMLBody, true, diagnoses.Values{Declared: "data", Observed: "data"})}
	if wantContainer == format.ContainerNone {
		return out
	}
	switch {
	case res.Container == format.ContainerNone:
		out = append(out, newAssertion(diagnoses.MagicMismatch, false, diagnoses.Values{
			Declared: wantContainer, Observed: "no gzip magic bytes (1f 8b) at the start"}))
	default:
		out = append(out, newAssertion(diagnoses.MagicMismatch, true, diagnoses.Values{Declared: wantContainer, Observed: res.Container}))
	}
	return out
}

func integrityStage(res facts.Result, exp Expected, withHashes bool) []model.Assertion {
	var out []model.Assertion
	check := func(algorithm, want, got string) {
		if want == "" {
			return
		}
		want = strings.ToLower(want)
		out = append(out, newAssertion(diagnoses.ChecksumMismatch, want == got, diagnoses.Values{Declared: want, Observed: got, Detail: algorithm}))
	}
	if withHashes {
		check("MD5", exp.MD5, res.MD5Hex())
		check("SHA-256", exp.SHA256, res.SHA256Hex())
		check("BLAKE3", exp.BLAKE3, res.RootHex())
	}
	if exp.Size != nil {
		out = append(out, newAssertion(diagnoses.SizeMismatch, *exp.Size == res.Size, diagnoses.Values{
			Declared: fmt.Sprint(*exp.Size), Observed: fmt.Sprint(res.Size)}))
	}
	return out
}

type structureFact struct {
	Format string `json:"format"`
	Valid  bool   `json:"valid"`
	Reason string `json:"reason"`
	Offset int64  `json:"offset"`
	Blank  int64  `json:"trailing_blank_lines"`
}

func structureFacts(res facts.Result) (container, content *structureFact) {
	for _, f := range res.Facts {
		if f.Kind != "structure" {
			continue
		}
		var s structureFact
		if json.Unmarshal(f.Value, &s) != nil {
			continue
		}
		if v, ok := format.Lookup(s.Format); ok && v.Compressed() {
			container = &s
		} else {
			content = &s
		}
	}
	return container, content
}

func structureStage(res facts.Result) []model.Assertion {
	container, content := structureFacts(res)
	var out []model.Assertion
	for _, s := range []*structureFact{container, content} {
		if s == nil {
			continue
		}
		observed := "valid"
		if !s.Valid {
			observed = fmt.Sprintf("%s (at byte %d)", s.Reason, s.Offset)
		}
		out = append(out, newAssertion(diagnoses.StructureInvalid, s.Valid, diagnoses.Values{Declared: "valid", Observed: observed, Detail: s.Format}))
		if !s.Valid {
			return out
		}
	}
	if content != nil && content.Format == format.ContentFastq {
		out = append(out, newAssertion(diagnoses.TrailingBlankLines, content.Blank == 0, diagnoses.Values{
			Declared: "0", Observed: fmt.Sprint(content.Blank)}))
	}
	return out
}

func formatStage(res facts.Result, declared string) []model.Assertion {
	var out []model.Assertion
	if res.Content == format.ContentUnknown {
		out = append(out, newAssertion(diagnoses.FormatUnknown, false, diagnoses.Values{Observed: res.Content}))
	} else {
		out = append(out, newAssertion(diagnoses.FormatUnknown, true, diagnoses.Values{Observed: res.Content}))
	}
	if declared != "" && format.Known(declared) {
		v, _ := format.Lookup(declared)
		observed := res.Content
		if v.Compressed() {
			observed = res.Container
		}
		out = append(out, newAssertion(diagnoses.FormatDeclared, observed == declared, diagnoses.Values{Declared: declared, Observed: observed}))
	}
	return out
}

type recordsFact struct {
	Records int64 `json:"records"`
	Bases   int64 `json:"bases"`
}

func recordsOf(res facts.Result) (recordsFact, bool) {
	f, ok := res.Fact("records")
	if !ok {
		return recordsFact{}, false
	}
	var r recordsFact
	return r, json.Unmarshal(f.Value, &r) == nil
}

func countStage(res facts.Result, exp Expected) []model.Assertion {
	var out []model.Assertion
	rec, ok := recordsOf(res)
	check := func(label string, want *int64, got int64) {
		if want == nil {
			return
		}
		observed := fmt.Sprint(got)
		if !ok {
			observed = "0 (no records read)"
		}
		out = append(out, newAssertion(diagnoses.CountMismatch, ok && *want == got, diagnoses.Values{
			Declared: fmt.Sprint(*want), Observed: observed, Detail: label}))
	}
	check("reads", exp.Reads, rec.Records)
	check("bases", exp.Bases, rec.Bases)
	return out
}

func describe(container, content string) string {
	switch {
	case content == "" || content == format.ContentUnknown:
		return container
	case container == format.ContainerNone:
		return content
	}
	return content + " in " + container
}

func extensionStage(res facts.Result, wantContainer, wantContent string) []model.Assertion {
	if wantContainer == format.ContainerNone && wantContent == "" {
		return nil
	}
	observedContent := res.Content
	if wantContent == "" {
		observedContent = ""
	}
	want := describe(wantContainer, wantContent)
	got := describe(res.Container, observedContent)
	return []model.Assertion{newAssertion(diagnoses.ExtensionMismatch, want == got, diagnoses.Values{Declared: want, Observed: got})}
}
