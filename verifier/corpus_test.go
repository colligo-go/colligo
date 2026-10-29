package verifier

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/internal/corpus"
	"github.com/colligo-go/colligo/secret"
)

const synthetic = "../testdata/synthetic"

func expectations(t *testing.T) map[string]corpus.Expect {
	t.Helper()
	metas, err := filepath.Glob(filepath.Join(synthetic, "*.meta.json"))
	if err != nil || len(metas) == 0 {
		t.Fatalf("no corpus metadata in %s: %v", synthetic, err)
	}
	out := map[string]corpus.Expect{}
	for _, m := range metas {
		raw, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Expect corpus.Expect `json:"expect"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		out[strings.TrimSuffix(filepath.Base(m), ".meta.json")] = doc.Expect
	}
	return out
}

func TestSyntheticCorpusBehavesAsTheGeneratorDeclares(t *testing.T) {
	want := expectations(t)
	names, skipped, _, err := Discover(context.Background(), secret.NewName(synthetic), false)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := CheckAll(context.Background(), names, Options{})
	if err != nil {
		t.Fatal(err)
	}
	checked := map[string]bool{}
	for _, f := range rep.Files {
		label := filepath.Base(f.Name.String())
		checked[label] = true
		exp, ok := want[label]
		if !ok {
			t.Errorf("%s was checked but has no metadata", label)
			continue
		}
		if exp.Skipped {
			t.Errorf("%s should have been skipped by discovery", label)
		}
		got := failed(f)
		sort.Strings(got)
		wantFailed := append([]string{}, exp.Failed...)
		sort.Strings(wantFailed)
		if !reflect.DeepEqual(got, wantFailed) && !(len(got) == 0 && len(wantFailed) == 0) {
			t.Errorf("%s: failed %v, generator declares %v", label, got, wantFailed)
		}
		if exp.Records == nil {
			continue
		}
		rec, ok := recordsOf(f.Result)
		if !ok {
			t.Errorf("%s: no records fact, generator wrote %d records", label, *exp.Records)
			continue
		}
		if rec.Records != *exp.Records {
			t.Errorf("%s: %d records, generator wrote %d", label, rec.Records, *exp.Records)
		}
		if exp.Bases != nil && rec.Bases != *exp.Bases {
			t.Errorf("%s: %d bases, generator wrote %d", label, rec.Bases, *exp.Bases)
		}
	}
	for label, exp := range want {
		if !exp.Skipped && !checked[label] {
			t.Errorf("%s was not checked", label)
		}
	}
	skippedData := 0
	for _, s := range skipped {
		label := filepath.Base(s)
		if strings.HasSuffix(label, ".meta.json") {
			continue
		}
		if exp, ok := want[label]; !ok || !exp.Skipped {
			t.Errorf("%s was skipped but the generator expects it checked", label)
		}
		skippedData++
	}
	for _, exp := range want {
		if exp.Skipped {
			skippedData--
		}
	}
	if skippedData != 0 {
		t.Errorf("skipped data files do not match the generator")
	}
}
