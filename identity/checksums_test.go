package identity

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/colligo-go/colligo/internal/exttool"
)

func TestEveryEmbeddedChecksumHasItsOriginAndDate(t *testing.T) {
	entries, err := KnownChecksums()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("the embedded checksum table is empty")
	}
	for i, e := range entries {
		if e.Label == "" || e.Asset == "" || !md5Hex.MatchString(e.MD5) {
			t.Errorf("entry %d is incomplete: %+v", i, e)
		}
		if !strings.HasPrefix(e.Origin, "https://") {
			t.Errorf("entry %d (%s) has no origin: %q", i, e.Asset, e.Origin)
		}
		day, err := time.Parse(time.DateOnly, e.Retrieved)
		if err != nil || day.After(time.Now().Add(24*time.Hour)) {
			t.Errorf("entry %d (%s) has no valid retrieval date: %q", i, e.Asset, e.Retrieved)
		}
	}
}

func TestChecksumTableRejectsWhatCannotBeTrusted(t *testing.T) {
	const good = `{"name":"n","file":"f.gtf.gz","md5":"0123456789abcdef0123456789abcdef","origin":"https://example.org/MD5SUMS","retrieved":"2026-10-31"}`
	if _, err := ParseChecksums([]byte("[" + good + "]")); err != nil {
		t.Fatalf("the good entry is rejected: %v", err)
	}
	cases := map[string]string{
		"no origin":     strings.Replace(good, `"origin":"https://example.org/MD5SUMS"`, `"origin":""`, 1),
		"http origin":   strings.Replace(good, "https://", "http://", 1),
		"no date":       strings.Replace(good, `"retrieved":"2026-10-31"`, `"retrieved":""`, 1),
		"bad date":      strings.Replace(good, "2026-10-31", "31/10/2026", 1),
		"no name":       strings.Replace(good, `"name":"n"`, `"name":""`, 1),
		"no file":       strings.Replace(good, `"file":"f.gtf.gz"`, `"file":""`, 1),
		"short md5":     strings.Replace(good, "0123456789abcdef0123456789abcdef", "0123", 1),
		"uppercase md5": strings.Replace(good, "0123456789abcdef0123456789abcdef", "0123456789ABCDEF0123456789ABCDEF", 1),
		"unknown field": strings.Replace(good, `"file"`, `"extra":1,"file"`, 1),
	}
	for name, entry := range cases {
		if _, err := ParseChecksums([]byte("[" + entry + "]")); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	_, err := ParseChecksums([]byte("[" + good + "," + good + "]"))
	var tableErr *TableError
	if !errors.As(err, &tableErr) || tableErr.Index != 1 {
		t.Errorf("a repeated md5: %v", err)
	}
}

func TestRecognizeChecksumIgnoresTheNameOfTheFile(t *testing.T) {
	entries := []Checksum{{Label: "a", Asset: "x.gtf.gz", MD5: "0123456789abcdef0123456789abcdef"}}
	if got, ok := RecognizeChecksum("0123456789abcdef0123456789abcdef", entries); !ok || got.Asset != "x.gtf.gz" {
		t.Errorf("got %+v, %v", got, ok)
	}
	if _, ok := RecognizeChecksum("ffffffffffffffffffffffffffffffff", entries); ok {
		t.Error("an unknown md5 was recognised")
	}
}

func TestVerifyRejectsAnUnusableEmbeddedTable(t *testing.T) {
	if err := Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestParseGencodeReadsTheDescriptionOfRealReleases(t *testing.T) {
	cases := []struct {
		description string
		want        Gencode
	}{
		{"evidence-based annotation of the human genome (GRCh38), version 44 (Ensembl 110)",
			Gencode{Label: "GENCODE 44", Release: "44", Organism: "human", Assembly: "GRCh38", Ensembl: "110", Date: "2023-03-01"}},
		{"evidence-based annotation of the mouse genome (GRCm39), version M33 (Ensembl 110)",
			Gencode{Label: "GENCODE M33", Release: "M33", Organism: "mouse", Assembly: "GRCm39", Ensembl: "110", Date: "2023-03-01"}},
		{"evidence-based annotation of the human genome, version 19",
			Gencode{Label: "GENCODE 19", Release: "19", Organism: "human", Date: "2023-03-01"}},
	}
	for _, c := range cases {
		got, ok := ParseGencode(GTFHeader{Description: c.description, Date: "2023-03-01"})
		if !ok || got != c.want {
			t.Errorf("%q: got %+v, %v; want %+v", c.description, got, ok, c.want)
		}
	}
	for _, other := range []string{
		"",
		"Ensembl GTF",
		"evidence-based annotation of the human genome (GRCh38), version latest",
		"evidence-based annotation of the human genome (GRCh38), version 44 (Ensembl 110) and more",
		"x evidence-based annotation of the human genome (GRCh38), version 44",
	} {
		if got, ok := ParseGencode(GTFHeader{Description: other}); ok {
			t.Errorf("%q was read as %+v", other, got)
		}
	}
}

const (
	generator = "../tools/gen_checksums.py"
	sources   = "../tools/checksums/sources.tsv"
)

func interpreter(t *testing.T) string {
	t.Helper()
	exe := os.Getenv(exttool.PythonVar)
	if exe == "" {
		exe = "python3"
	}
	path, err := exec.LookPath(exe)
	if err != nil {
		if os.Getenv(exttool.PythonVar) != "" || os.Getenv(exttool.RequirePythonVar) != "" {
			t.Fatalf("python is required to check the generator: %v", err)
		}
		t.Skipf("python is needed to check the generator: %v", err)
	}
	return path
}

func generate(t *testing.T, python, sourcesFile string) string {
	t.Helper()
	out, err := exec.Command(python, "-I", generator, sourcesFile).Output()
	if err != nil {
		t.Fatalf("the generator failed: %v", err)
	}
	return string(out)
}

func TestGeneratorRebuildsTheEmbeddedTableByteForByteInAnyOrder(t *testing.T) {
	python := interpreter(t)
	embedded, err := os.ReadFile("checksums.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := generate(t, python, sources); got != string(embedded) {
		t.Fatal("identity/checksums.json is not what tools/gen_checksums.py builds from tools/checksums/")
	}
	lines, err := os.ReadFile(sources)
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(lines)), "\n")
	slices.Reverse(rows)
	for i, row := range rows {
		fields := strings.Split(row, "\t")
		abs, err := filepath.Abs(filepath.Join(filepath.Dir(sources), fields[3]))
		if err != nil {
			t.Fatal(err)
		}
		fields[3] = abs
		rows[i] = strings.Join(fields, "\t")
	}
	reversed := filepath.Join(t.TempDir(), "sources.tsv")
	if err := os.WriteFile(reversed, []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := generate(t, python, reversed); got != string(embedded) {
		t.Error("the order of the sources changes the table")
	}
}

func TestGeneratorRefusesAnMD5PublishedForTwoFiles(t *testing.T) {
	python := interpreter(t)
	dir := t.TempDir()
	sum := strings.Repeat("a", 32)
	if err := os.WriteFile(filepath.Join(dir, "sums"), []byte(sum+"  one.gtf.gz\n"+sum+"  two.gtf.gz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	row := "R\thttps://example.org/MD5SUMS\t2026-10-31\tsums\n"
	if err := os.WriteFile(filepath.Join(dir, "sources.tsv"), []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(python, "-I", generator, filepath.Join(dir, "sources.tsv")).Run(); err == nil {
		t.Error("the generator accepted a repeated md5")
	}
}

func TestTableErrorNamesTheEntryAndWhatIsWrongWithIt(t *testing.T) {
	const good = `{"name":"n","file":"f.gtf.gz","md5":"0123456789abcdef0123456789abcdef","origin":"https://example.org/MD5SUMS","retrieved":"2026-10-31"}`
	bad := strings.Replace(good, `"name":"n"`, `"name":""`, 1)
	cases := map[string]struct {
		table string
		want  string
	}{
		"no name":      {"[" + good + "," + bad + "]", "identity: checksum table entry 1: no name"},
		"repeated md5": {"[" + good + "," + strings.Replace(good, `"file":"f.gtf.gz"`, `"file":"g.gtf.gz"`, 1) + "]", "identity: checksum table entry 1: md5 0123456789abcdef0123456789abcdef repeats entry 0"},
	}
	for name, tc := range cases {
		_, err := ParseChecksums([]byte(tc.table))
		var tableErr *TableError
		if !errors.As(err, &tableErr) {
			t.Fatalf("%s: error %v", name, err)
		}
		if err.Error() != tc.want {
			t.Errorf("%s: message %q, want %q", name, err.Error(), tc.want)
		}
	}
}
