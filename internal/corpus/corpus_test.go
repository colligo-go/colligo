package corpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

var record = flag.Bool("record", false, "rewrite testdata/synthetic from the generator")

const dir = "../../testdata/synthetic"

type meta struct {
	Generator   string `json:"generator"`
	Description string `json:"description"`
	Size        int    `json:"size"`
	SHA256      string `json:"sha256"`
	RecordedAt  string `json:"recorded_at"`
	Expect      Expect `json:"expect"`
}

func digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func readMeta(t *testing.T, label string) (meta, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, label+".meta.json"))
	if err != nil {
		return meta{}, false
	}
	var m meta
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s.meta.json: %v", label, err)
	}
	return m, true
}

func metaFor(t *testing.T, f Fixture) meta {
	t.Helper()
	m := meta{Generator: "internal/corpus", Description: f.Description, Size: len(f.Data), SHA256: digest(f.Data), Expect: f.Expect,
		RecordedAt: time.Now().UTC().Format(time.RFC3339)}
	if old, ok := readMeta(t, f.Label); ok && old.SHA256 == m.SHA256 {
		m.RecordedAt = old.RecordedAt
	}
	return m
}

func TestRecord(t *testing.T) {
	if !*record {
		t.Skip("run with -record to rewrite the corpus")
	}
	for _, f := range Generate() {
		m := metaFor(t, f)
		raw, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.Label), f.Data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.Label+".meta.json"), append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVersionedCorpusMatchesTheGenerator(t *testing.T) {
	if *record {
		t.Skip("recording")
	}
	want := map[string]bool{".gitkeep": true}
	for _, f := range Generate() {
		want[f.Label], want[f.Label+".meta.json"] = true, true
		data, err := os.ReadFile(filepath.Join(dir, f.Label))
		if err != nil {
			t.Fatalf("%s missing: run go test ./internal/corpus -record", f.Label)
		}
		if !bytes.Equal(data, f.Data) {
			t.Errorf("%s differs from the generator: re-record and review the diff", f.Label)
		}
		m, ok := readMeta(t, f.Label)
		if !ok {
			t.Fatalf("%s.meta.json missing", f.Label)
		}
		if m.SHA256 != digest(data) || m.Size != len(data) {
			t.Errorf("%s.meta.json does not describe the file on disk", f.Label)
		}
		if !reflect.DeepEqual(m.Expect, f.Expect) {
			t.Errorf("%s.meta.json expectations differ from the generator", f.Label)
		}
		if m.RecordedAt == "" || m.Generator == "" {
			t.Errorf("%s.meta.json lacks provenance", f.Label)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var stray []string
	for _, e := range entries {
		if !want[e.Name()] {
			stray = append(stray, e.Name())
		}
	}
	sort.Strings(stray)
	if len(stray) > 0 {
		t.Errorf("files in testdata/synthetic that the generator does not produce: %v", stray)
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	a, b := Generate(), Generate()
	for i := range a {
		if !bytes.Equal(a[i].Data, b[i].Data) {
			t.Fatalf("%s is not deterministic", a[i].Label)
		}
	}
}
