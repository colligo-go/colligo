package verifier

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
)

func fastqText(prefix string, reads int, seq string) []byte {
	var b bytes.Buffer
	for i := 0; i < reads; i++ {
		fmt.Fprintf(&b, "@%s.%d/1\n%s\n+\n%s\n", prefix, i+1, seq, strings.Repeat("F", len(seq)))
	}
	return b.Bytes()
}

func gzipped(t *testing.T, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write(data)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func write(t *testing.T, dir, base string, data []byte) secret.Name {
	t.Helper()
	p := filepath.Join(dir, base)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return secret.NewName(p)
}

func run(t *testing.T, opts Options, names ...secret.Name) Report {
	t.Helper()
	rep, err := CheckAll(context.Background(), names, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func failed(rep *FileReport) []string {
	var ids []string
	for _, a := range rep.Assertions {
		if !a.Passed && !a.Skipped {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

func has(ids []string, id string) bool {
	for _, i := range ids {
		if i == id {
			return true
		}
	}
	return false
}

func TestCleanPairIsVerified(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "s_1.fastq.gz", gzipped(t, fastqText("SRR1", 500, "ACGTACGT")))
	r2 := write(t, dir, "s_2.fastq.gz", gzipped(t, fastqText("SRR1", 500, "ACGTACGT")))
	rep := run(t, Options{}, r1, r2)
	for _, f := range rep.Files {
		if f.Status != StatusVerified || len(failed(f)) != 0 {
			t.Fatalf("%s: %s %v", f.Name, f.Status, failed(f))
		}
	}
	if rep.ExitCode(false) != ExitOK {
		t.Fatal("exit code")
	}
}

func TestMissingReadInR2IsBlockingExit3(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "s_R1.fastq.gz", gzipped(t, fastqText("SRR1", 500, "ACGTACGT")))
	r2 := write(t, dir, "s_R2.fastq.gz", gzipped(t, fastqText("SRR1", 499, "ACGTACGT")))
	rep := run(t, Options{}, r1, r2)
	for _, f := range rep.Files {
		if !has(failed(f), diagnoses.PairCount) || f.Status != StatusInvalid {
			t.Fatalf("%s: %s %v", f.Name, f.Status, failed(f))
		}
	}
	if rep.ExitCode(false) != ExitIncomplete {
		t.Fatalf("exit code %d", rep.ExitCode(false))
	}
	if rep.ExitCode(true) != ExitOK {
		t.Fatal("permissive must exit 0")
	}
}

func TestPairWithSameCountButDifferentIDs(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "s_1.fastq", fastqText("SRR1", 100, "ACGT"))
	r2 := write(t, dir, "s_2.fastq", fastqText("SRR2", 100, "ACGT"))
	rep := run(t, Options{}, r1, r2)
	if !has(failed(rep.Files[0]), diagnoses.PairIDs) || rep.ExitCode(false) != ExitIncomplete {
		t.Fatalf("%v", failed(rep.Files[0]))
	}
}

func TestTruncatedGzipIsBlockingAndSkipsWhatDependsOnIt(t *testing.T) {
	dir := t.TempDir()
	data := gzipped(t, fastqText("SRR1", 2000, "ACGTACGT"))
	n := write(t, dir, "s.fastq.gz", data[:len(data)/2])
	expected := int64(2000)
	rep := run(t, Options{Expected: Expected{Reads: &expected}}, n)
	f := rep.Files[0]
	if f.Status != StatusInvalid || !has(failed(f), diagnoses.StructureInvalid) {
		t.Fatalf("%s %v", f.Status, failed(f))
	}
	var skipped []string
	for _, a := range f.Assertions {
		if a.Skipped {
			skipped = append(skipped, a.ID)
			if a.SkippedBy != diagnoses.StructureInvalid {
				t.Errorf("%s skipped by %s", a.ID, a.SkippedBy)
			}
		}
	}
	if !has(skipped, diagnoses.CountMismatch) {
		t.Fatalf("count assertion must be skipped: %v", skipped)
	}
	if rep.ExitCode(false) != ExitIncomplete {
		t.Fatal("exit code")
	}
}

func TestHTMLRenamedAsGTFGzIsFatal(t *testing.T) {
	dir := t.TempDir()
	n := write(t, dir, "annotation.gtf.gz", []byte("<!DOCTYPE html><html><body>403</body></html>"))
	rep := run(t, Options{}, n)
	f := rep.Files[0]
	if !has(failed(f), diagnoses.HTMLBody) || rep.ExitCode(false) != ExitFatal {
		t.Fatalf("%v exit %d", failed(f), rep.ExitCode(false))
	}
}

func TestPlainTextNamedGzIsFatalByMagic(t *testing.T) {
	dir := t.TempDir()
	n := write(t, dir, "reads.fastq.gz", fastqText("SRR1", 10, "ACGT"))
	rep := run(t, Options{}, n)
	if !has(failed(rep.Files[0]), diagnoses.MagicMismatch) || rep.ExitCode(false) != ExitFatal {
		t.Fatalf("%v", failed(rep.Files[0]))
	}
}

func TestExpectedChecksum(t *testing.T) {
	dir := t.TempDir()
	data := fastqText("SRR1", 50, "ACGT")
	n := write(t, dir, "s.fastq", data)
	sum := md5.Sum(data)
	good := run(t, Options{Expected: Expected{MD5: hex.EncodeToString(sum[:])}}, n)
	if len(failed(good.Files[0])) != 0 {
		t.Fatalf("%v", failed(good.Files[0]))
	}
	bad := run(t, Options{Expected: Expected{MD5: strings.Repeat("0", 32)}}, n)
	if !has(failed(bad.Files[0]), diagnoses.ChecksumMismatch) || bad.ExitCode(false) != ExitFatal {
		t.Fatalf("%v", failed(bad.Files[0]))
	}
	size := int64(len(data) + 1)
	wrongSize := run(t, Options{Expected: Expected{Size: &size}}, n)
	if !has(failed(wrongSize.Files[0]), diagnoses.SizeMismatch) {
		t.Fatalf("%v", failed(wrongSize.Files[0]))
	}
}

func TestDeclaredCountIsBlocking(t *testing.T) {
	dir := t.TempDir()
	n := write(t, dir, "s.fastq", fastqText("SRR1", 50, "ACGT"))
	reads, bases := int64(50), int64(1000)
	rep := run(t, Options{Expected: Expected{Reads: &reads, Bases: &bases}}, n)
	f := rep.Files[0]
	if got := failed(f); len(got) != 1 || got[0] != diagnoses.CountMismatch {
		t.Fatalf("%v", got)
	}
	if rep.ExitCode(false) != ExitIncomplete {
		t.Fatal("exit code")
	}
}

func TestExtensionDivergenceIsAdvisory(t *testing.T) {
	dir := t.TempDir()
	n := write(t, dir, "genome.fasta", fastqText("SRR1", 5, "ACGT"))
	rep := run(t, Options{}, n)
	f := rep.Files[0]
	if got := failed(f); len(got) != 1 || got[0] != diagnoses.ExtensionMismatch {
		t.Fatalf("%v", got)
	}
	if f.Status != StatusVerified || rep.ExitCode(false) != ExitOK {
		t.Fatalf("advisory must not fail the file: %s", f.Status)
	}
}

func TestDeclaredFormatMismatchIsBlocking(t *testing.T) {
	dir := t.TempDir()
	n := write(t, dir, "x.dat", fastqText("SRR1", 5, "ACGT"))
	rep := run(t, Options{Format: "fasta"}, n)
	if !has(failed(rep.Files[0]), diagnoses.FormatDeclared) && !has(failed(rep.Files[0]), diagnoses.StructureInvalid) {
		t.Fatalf("%v", failed(rep.Files[0]))
	}
	if rep.ExitCode(false) != ExitIncomplete {
		t.Fatal("exit code")
	}
}

func TestUnreadableFileIsFatal(t *testing.T) {
	rep := run(t, Options{}, secret.NewName(filepath.Join(t.TempDir(), "missing.fastq")))
	f := rep.Files[0]
	if f.Status != StatusUnreadable || rep.ExitCode(false) != ExitFatal {
		t.Fatalf("%s", f.Status)
	}
	if strings.Contains(f.Assertions[0].Observation, os.TempDir()) {
		t.Fatalf("observation leaks the path: %s", f.Assertions[0].Observation)
	}
}

func TestValidateSkipsHashesPairsAndCounts(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "s_1.fastq", fastqText("SRR1", 10, "ACGT"))
	r2 := write(t, dir, "s_2.fastq", fastqText("SRR1", 9, "ACGT"))
	reads := int64(1)
	rep := run(t, Options{Structure: true, Expected: Expected{Reads: &reads}}, r1, r2)
	for _, f := range rep.Files {
		if f.Status != StatusValid || f.MD5 != "" || len(failed(f)) != 0 {
			t.Fatalf("%s %v", f.Status, failed(f))
		}
	}
}

func TestQuickIsNeverVerified(t *testing.T) {
	dir := t.TempDir()
	good := write(t, dir, "a.fastq", fastqText("SRR1", 10, "ACGT"))
	cut := write(t, dir, "b.fastq", fastqText("SRR1", 10, "ACGT")[:50])
	rep := run(t, Options{Quick: true}, good, cut)
	if rep.Files[0].Status != StatusQuick {
		t.Fatalf("%s", rep.Files[0].Status)
	}
	if rep.Files[1].Status != StatusInvalid {
		t.Fatalf("incomplete last line must be caught: %s %v", rep.Files[1].Status, failed(rep.Files[1]))
	}
}

func TestQuickBGZFNeedsEOFBlock(t *testing.T) {
	header := []byte{0x1f, 0x8b, 8, 4, 0, 0, 0, 0, 0, 0xff, 6, 0, 'B', 'C', 2, 0, 0x1b, 0}
	body := bytes.Repeat([]byte{7}, 200)
	dir := t.TempDir()
	withEOF := write(t, dir, "a.bgz", append(append(append([]byte{}, header...), body...), format.BGZFEOFBlock()...))
	without := write(t, dir, "b.bgz", append(append([]byte{}, header...), body...))
	rep := run(t, Options{Quick: true}, withEOF, without)
	if has(failed(rep.Files[0]), diagnoses.StructureInvalid) {
		t.Fatalf("EOF block present but rejected: %v", failed(rep.Files[0]))
	}
	if !has(failed(rep.Files[1]), diagnoses.StructureInvalid) {
		t.Fatalf("missing EOF block not detected: %v", failed(rep.Files[1]))
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "s_1.fastq.gz", gzipped(t, fastqText("SRR1", 3, "ACGT")))
	write(t, dir, "genome.fa", []byte(">chr1\nACGT\n"))
	write(t, dir, "notes.txt", []byte("hello"))
	write(t, dir, "page.gtf.gz", []byte("<html>"))
	write(t, dir, ".hidden.fastq", fastqText("X", 1, "A"))
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	write(t, sub, "deep.fastq", fastqText("D", 1, "A"))

	names, skipped, err := Discover(context.Background(), secret.NewName(dir), false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range names {
		got = append(got, filepath.Base(n.String()))
	}
	want := []string{"genome.fa", "page.gtf.gz", "s_1.fastq.gz"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
	if len(skipped) != 1 || filepath.Base(skipped[0]) != "notes.txt" {
		t.Fatalf("skipped %v", skipped)
	}
	deep, _, err := Discover(context.Background(), secret.NewName(dir), true)
	if err != nil || len(deep) != 4 {
		t.Fatalf("recursive: %d %v", len(deep), err)
	}
}

func TestEveryAssertionHasACatalogueEntry(t *testing.T) {
	for _, id := range AssertionIDs() {
		e, ok := diagnoses.Lookup(id)
		if !ok {
			t.Errorf("%s has no catalogue entry", id)
			continue
		}
		if e.Mode == "" || e.Observation == "" || e.Cause == "" || e.Action == "" || e.Severity == "" {
			t.Errorf("%s: incomplete entry %+v", id, e)
		}
	}
	for _, id := range diagnoses.IDs() {
		found := false
		for _, a := range AssertionIDs() {
			found = found || a == id
		}
		if !found {
			t.Errorf("catalogue entry %s is never produced", id)
		}
	}
}

func TestReportRendering(t *testing.T) {
	dir := t.TempDir()
	n := write(t, dir, "s.fastq", fastqText("SRR1", 5, "ACGT")[:40])
	rep := run(t, Options{}, n)
	var text bytes.Buffer
	rep.WriteText(&text)
	for _, want := range []string{"BLOCKING", "structure_invalid", "probable cause:", "suggested action:"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text report lacks %q:\n%s", want, text.String())
		}
	}
	var js bytes.Buffer
	if err := rep.WriteJSON(&js, rep.ExitCode(false)); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		SchemaVersion int `json:"schema_version"`
		ExitCode      int `json:"exit_code"`
		Files         []struct {
			Path       string            `json:"path"`
			Assertions []model.Assertion `json:"assertions"`
		} `json:"files"`
	}
	if err := json.Unmarshal(js.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != SchemaVersion || decoded.ExitCode != ExitIncomplete || decoded.Files[0].Path != n.String() {
		t.Fatalf("%+v", decoded)
	}
}

func TestFactsDoNotDependOnNamePathOrMtime(t *testing.T) {
	data := gzipped(t, fastqText("SRR1", 300, "ACGTACGT"))
	a := write(t, t.TempDir(), "first_name.fastq.gz", data)
	b := write(t, t.TempDir(), "completely-different.dat", data)
	old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(b.String(), old, old); err != nil {
		t.Fatal(err)
	}
	rep := run(t, Options{}, a, b)
	first, second := rep.Files[0].Result, rep.Files[1].Result
	ja, _ := json.Marshal(first.Facts)
	jb, _ := json.Marshal(second.Facts)
	if !bytes.Equal(ja, jb) || first.Root != second.Root {
		t.Fatal("facts changed with the name, the path or the mtime")
	}
}

func TestTrailingBlankLinesAreAdvisoryAndDoNotFailTheFile(t *testing.T) {
	dir := t.TempDir()
	n := write(t, dir, "s.fastq", append(fastqText("SRR1", 5, "ACGT"), '\n', '\n'))
	rep := run(t, Options{}, n)
	f := rep.Files[0]
	if got := failed(f); len(got) != 1 || got[0] != diagnoses.TrailingBlankLines {
		t.Fatalf("%v", got)
	}
	if f.Status != StatusVerified || rep.ExitCode(false) != ExitOK {
		t.Fatalf("%s exit %d", f.Status, rep.ExitCode(false))
	}
}
