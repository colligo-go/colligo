package verifier

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
)

func count(n int64) *int64 { return &n }

func reads(prefix string, n int, length int) []byte {
	var b bytes.Buffer
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "@%s.%d\n%s\n+\n%s\n", prefix, i+1, strings.Repeat("A", length), strings.Repeat("F", length))
	}
	return b.Bytes()
}

func artifact(name string) model.Artifact {
	return model.Artifact{Name: secret.NewName(name), Format: "fastq"}
}

func sample(accession, layout, status string, readCount, baseCount *int64, files ...string) model.Sample {
	s := model.Sample{
		ID: accession, Accession: accession, Layout: layout, SourceStatus: status,
		Declared: model.RunCounts{ReadCount: readCount, BaseCount: baseCount},
	}
	for _, f := range files {
		s.Artifacts = append(s.Artifacts, artifact(f))
	}
	return s
}

func against(t *testing.T, decl Declaration, names ...secret.Name) Report {
	t.Helper()
	if decl.Source == "" {
		decl.Source, decl.Accession = "ena", "TEST"
	}
	rep, err := CheckAgainstSource(context.Background(), names, decl, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func runAssertion(t *testing.T, rep Report, run int, id string) model.Assertion {
	t.Helper()
	for _, a := range rep.Source.Runs[run].Assertions {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("run %d has no %s assertion: %+v", run, id, rep.Source.Runs[run].Assertions)
	return model.Assertion{}
}

func sourceFailures(rep Report) []string {
	var ids []string
	for _, a := range rep.Source.Assertions {
		if !a.Passed && !a.Skipped {
			ids = append(ids, a.ID)
		}
	}
	for _, r := range rep.Source.Runs {
		for _, a := range r.Assertions {
			if !a.Passed && !a.Skipped {
				ids = append(ids, a.ID)
			}
		}
	}
	return ids
}

func TestSourceBaseCountOverEveryFileDecides(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "SRR1_1.fastq", reads("SRR1", 10, 50))
	r2 := write(t, dir, "SRR1_2.fastq", reads("SRR1", 10, 50))
	decl := Declaration{Samples: []model.Sample{sample("SRR1", "PAIRED", "public", count(10), count(1000), "SRR1_1.fastq.gz", "SRR1_2.fastq.gz")}}

	rep := against(t, decl, r1, r2)
	if code := rep.ExitCode(false); code != ExitOK {
		t.Fatalf("exit %d, failures %v", code, sourceFailures(rep))
	}
	if a := runAssertion(t, rep, 0, diagnoses.CountMismatch); !a.Passed || a.Declared != "1000" || a.Observed != "1000" {
		t.Fatalf("base count assertion %+v", a)
	}

	short := write(t, dir, "SRR1_2.fastq", reads("SRR1", 10, 1))
	rep = against(t, decl, r1, short)
	if code := rep.ExitCode(false); code != ExitIncomplete {
		t.Fatalf("shortened read 2: exit %d, want 3", code)
	}
	a := runAssertion(t, rep, 0, diagnoses.CountMismatch)
	if a.Passed || a.Severity != model.Blocking || a.Observed != "510" || !strings.Contains(a.Observation, "2 file(s) of run SRR1") {
		t.Fatalf("base count assertion %+v", a)
	}
	for _, f := range rep.Files {
		if f.Status != StatusInvalid || !has(failed(f), diagnoses.SourceRunFailed) || has(failed(f), diagnoses.CountMismatch) {
			t.Fatalf("%s must carry the run failure: %s %v", f.Name, f.Status, failed(f))
		}
	}
	if rc := runAssertion(t, rep, 0, diagnoses.ReadCountUnexplained); !rc.Skipped || rc.SkippedBy != diagnoses.CountMismatch {
		t.Fatalf("read_count must not be judged once the bases fail: %+v", rc)
	}
}

func TestSourceReadCountReadings(t *testing.T) {
	for _, tc := range []struct {
		label     string
		files     map[string]int
		readCount int64
		want      string
	}{
		{"each file", map[string]int{"R_1.fastq": 7, "R_2.fastq": 7}, 7, "7 in each file"},
		{"sum", map[string]int{"R_1.fastq": 7, "R_2.fastq": 7}, 14, "14, the sum of the files"},
		{"sum of three", map[string]int{"R.fastq": 2, "R_1.fastq": 7, "R_2.fastq": 7}, 16, "16, the sum of the files"},
		{"pairs plus unpaired", map[string]int{"R.fastq": 1, "R_1.fastq": 7, "R_2.fastq": 7}, 8, "8, the pairs plus the unpaired files"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			dir := t.TempDir()
			var names []secret.Name
			var declared []string
			var bases int64
			for name, n := range tc.files {
				names = append(names, write(t, dir, name, reads("R", n, 4)))
				declared = append(declared, name+".gz")
				bases += int64(n * 4)
			}
			rep := against(t, Declaration{Samples: []model.Sample{sample("R", "PAIRED", "public", count(tc.readCount), count(bases), declared...)}}, names...)
			a := runAssertion(t, rep, 0, diagnoses.ReadCountUnexplained)
			if !a.Passed || a.Observed != tc.want || rep.ExitCode(false) != ExitOK {
				t.Fatalf("got %+v, exit %d", a, rep.ExitCode(false))
			}
		})
	}
}

func TestSourceReadCountNoReadingMatchesIsAdvisory(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "R_1.fastq", reads("R", 7, 4))
	r2 := write(t, dir, "R_2.fastq", reads("R", 7, 4))
	rep := against(t, Declaration{Samples: []model.Sample{sample("R", "PAIRED", "public", count(9), count(56), "R_1.fastq.gz", "R_2.fastq.gz")}}, r1, r2)
	a := runAssertion(t, rep, 0, diagnoses.ReadCountUnexplained)
	if a.Passed || a.Severity != model.Advisory || !strings.Contains(a.Observed, "per file 7, 7, sum 14") {
		t.Fatalf("got %+v", a)
	}
	if code := rep.ExitCode(false); code != ExitOK {
		t.Fatalf("an unexplained read_count with matching bases must not block: exit %d", code)
	}
}

func TestSourceUndeclaredCountsAreSkippedNotFailed(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "R_1.fastq", reads("R", 5, 4))
	r2 := write(t, dir, "R_2.fastq", reads("R", 5, 4))
	rep := against(t, Declaration{Samples: []model.Sample{sample("R", "PAIRED", "public", nil, count(40), "R_1.fastq.gz", "R_2.fastq.gz")}}, r1, r2)
	rc := runAssertion(t, rep, 0, diagnoses.ReadCountUnexplained)
	if !rc.Skipped || rc.SkippedBy != NotDeclared || rc.Declared != "not declared" {
		t.Fatalf("read_count %+v", rc)
	}
	if bc := runAssertion(t, rep, 0, diagnoses.CountMismatch); !bc.Passed {
		t.Fatalf("base_count must still decide: %+v", bc)
	}
	if code := rep.ExitCode(false); code != ExitOK {
		t.Fatalf("exit %d", code)
	}

	short := write(t, dir, "R_2.fastq", reads("R", 5, 1))
	rep = against(t, Declaration{Samples: []model.Sample{sample("R", "PAIRED", "public", nil, count(40), "R_1.fastq.gz", "R_2.fastq.gz")}}, r1, short)
	if code := rep.ExitCode(false); code != ExitIncomplete {
		t.Fatalf("without read_count the bases still block: exit %d", code)
	}

	rep = against(t, Declaration{Samples: []model.Sample{sample("R", "PAIRED", "", count(5), nil, "R_1.fastq.gz", "R_2.fastq.gz")}}, r1, r2)
	for _, id := range []string{diagnoses.CountMismatch, diagnoses.SourceStatus} {
		if a := runAssertion(t, rep, 0, id); !a.Skipped || a.SkippedBy != NotDeclared {
			t.Errorf("%s %+v", id, a)
		}
	}
	if code := rep.ExitCode(false); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}

func TestSourceLayoutMismatch(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "R_1.fastq", reads("R", 5, 4))
	r2 := write(t, dir, "R_2.fastq", reads("R", 5, 4))
	rep := against(t, Declaration{Samples: []model.Sample{sample("R", "SINGLE", "public", count(10), count(40), "R_1.fastq.gz", "R_2.fastq.gz")}}, r1, r2)
	a := runAssertion(t, rep, 0, diagnoses.LayoutMismatch)
	if a.Passed || a.Severity != model.Advisory || a.Observed != "R_1.fastq.gz and R_2.fastq.gz" || rep.ExitCode(false) != ExitOK {
		t.Fatalf("got %+v exit %d", a, rep.ExitCode(false))
	}

	alone := write(t, dir, "S.fastq", reads("S", 5, 4))
	rep = against(t, Declaration{Samples: []model.Sample{sample("S", "PAIRED", "public", count(5), count(20), "S.fastq.gz")}}, alone)
	for _, a := range rep.Source.Runs[0].Assertions {
		if a.ID == diagnoses.LayoutMismatch {
			t.Fatalf("a paired label on one file is not judged: %+v", a)
		}
	}
}

func TestSourceSuppressedRunWithoutFilesIsReported(t *testing.T) {
	rep := against(t, Declaration{Samples: []model.Sample{sample("ERR1", "PAIRED", "suppressed", count(10), count(100))}})
	if len(rep.Source.Runs) != 1 {
		t.Fatalf("the run must be in the report: %+v", rep.Source)
	}
	a := runAssertion(t, rep, 0, diagnoses.SourceStatus)
	if a.Passed || a.Severity != model.Blocking || a.Observed != "suppressed" {
		t.Fatalf("status %+v", a)
	}
	if bc := runAssertion(t, rep, 0, diagnoses.CountMismatch); !bc.Skipped || bc.SkippedBy != NoFilesListed {
		t.Fatalf("base count %+v", bc)
	}
	if code := rep.ExitCode(false); code != ExitIncomplete {
		t.Fatalf("exit %d", code)
	}
	if run := rep.Source.Runs[0]; run.Accession != "ERR1" || run.Layout != "PAIRED" || run.Status != "suppressed" || len(run.Files) != 0 {
		t.Errorf("run %+v", run)
	}
}

func TestSourceMissingAndUnlistedFilesAppear(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "R_1.fastq", reads("R", 5, 4))
	other := write(t, dir, "other.fastq", reads("O", 5, 4))
	rep := against(t, Declaration{Samples: []model.Sample{sample("R", "PAIRED", "public", count(5), count(40), "R_1.fastq.gz", "R_2.fastq.gz")}}, r1, other)
	missing := runAssertion(t, rep, 0, diagnoses.SourceFileMissing)
	if missing.Passed || missing.Declared != "R_2.fastq.gz" {
		t.Fatalf("missing %+v", missing)
	}
	if bc := runAssertion(t, rep, 0, diagnoses.CountMismatch); !bc.Skipped || bc.SkippedBy != diagnoses.SourceFileMissing {
		t.Fatalf("base count must wait for every file: %+v", bc)
	}
	if !has(failed(rep.Files[1]), diagnoses.SourceFileUnlisted) {
		t.Fatalf("other.fastq: %v", failed(rep.Files[1]))
	}
	if rep.Source.Runs[0].Files[0].Local == nil || rep.Source.Runs[0].Files[1].Local != nil {
		t.Fatalf("files %+v", rep.Source.Runs[0].Files)
	}
	if code := rep.ExitCode(false); code != ExitIncomplete {
		t.Fatalf("exit %d", code)
	}
}

func TestSourceExactNameIsHeldToThePublishedChecksum(t *testing.T) {
	dir := t.TempDir()
	data := reads("R", 5, 4)
	sum := md5.Sum(data)
	name := write(t, dir, "R.fastq", data)
	good := sample("R", "SINGLE", "public", count(5), count(20), "R.fastq")
	good.Artifacts[0].MD5, good.Artifacts[0].Size = hex.EncodeToString(sum[:]), int64(len(data))
	rep := against(t, Declaration{Samples: []model.Sample{good}}, name)
	if code := rep.ExitCode(false); code != ExitOK {
		t.Fatalf("exit %d %v", code, failed(rep.Files[0]))
	}

	bad := good
	bad.Artifacts = []model.Artifact{good.Artifacts[0]}
	bad.Artifacts[0].MD5 = strings.Repeat("0", 32)
	rep = against(t, Declaration{Samples: []model.Sample{bad}}, name)
	if !has(failed(rep.Files[0]), diagnoses.ChecksumMismatch) || rep.ExitCode(false) != ExitFatal {
		t.Fatalf("exit %d %v", rep.ExitCode(false), failed(rep.Files[0]))
	}
	if bc := runAssertion(t, rep, 0, diagnoses.CountMismatch); !bc.Skipped || bc.SkippedBy != diagnoses.ChecksumMismatch {
		t.Fatalf("base count %+v", bc)
	}

	decompressed := sample("R", "SINGLE", "public", count(5), count(20), "R.fastq.gz")
	decompressed.Artifacts[0].MD5 = strings.Repeat("0", 32)
	rep = against(t, Declaration{Samples: []model.Sample{decompressed}}, name)
	if code := rep.ExitCode(false); code != ExitOK {
		t.Fatalf("a decompressed copy is not held to the MD5 of the compressed file: exit %d %v", code, failed(rep.Files[0]))
	}
}

func TestSourceCopiesOfOneDeclaredFile(t *testing.T) {
	dir := t.TempDir()
	data := reads("R", 5, 4)
	exact := write(t, dir, "R.fastq.gz", gzipped(t, data))
	plain := write(t, dir, "R.fastq", data)
	s := sample("R", "SINGLE", "public", count(5), count(20), "R.fastq.gz")
	rep := against(t, Declaration{Samples: []model.Sample{s}}, exact, plain)
	if rep.Source.Runs[0].Files[0].Local == nil || filepath.Base(rep.Source.Runs[0].Files[0].Local.String()) != "R.fastq.gz" {
		t.Fatalf("the exact name must stand for the declared file: %+v", rep.Source.Runs[0].Files)
	}
	if !has(failed(rep.Files[1]), diagnoses.SourceFileUnlisted) || rep.ExitCode(false) != ExitOK {
		t.Fatalf("the decompressed copy: %v exit %d", failed(rep.Files[1]), rep.ExitCode(false))
	}

	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	one := write(t, mkdir(t, a), "R.fastq", data)
	two := write(t, mkdir(t, b), "R.fastq", data)
	rep = against(t, Declaration{Samples: []model.Sample{s}}, one, two)
	for _, f := range rep.Files {
		if !has(failed(f), diagnoses.SourceFileAmbiguous) {
			t.Fatalf("%s: %v", f.Name, failed(f))
		}
	}
	if bc := runAssertion(t, rep, 0, diagnoses.CountMismatch); !bc.Skipped || bc.SkippedBy != diagnoses.SourceFileAmbiguous {
		t.Fatalf("base count %+v", bc)
	}
	if rep.ExitCode(false) != ExitIncomplete {
		t.Fatalf("exit %d", rep.ExitCode(false))
	}
}

func TestSourceIssuesAppearInTheReport(t *testing.T) {
	rep := against(t, Declaration{
		Samples: []model.Sample{sample("R", "PAIRED", "public", nil, nil)},
		Issues: []model.ValidationIssue{
			{Severity: model.Advisory, Code: "empty_field", Subject: "R", Message: "read_count is empty"},
			{Severity: model.Blocking, Code: "bad_md5", Subject: "R2", Message: "fastq_md5 has an entry that is not 32 hex digits"},
		},
	})
	got := sourceFailures(rep)
	if !has(got, diagnoses.SourceFieldEmpty) || !has(got, diagnoses.SourceRowRejected) || rep.ExitCode(false) != ExitIncomplete {
		t.Fatalf("failures %v exit %d", got, rep.ExitCode(false))
	}
}

func TestSourceReportJSON(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "R_1.fastq", reads("R", 5, 4))
	rep := against(t, Declaration{Samples: []model.Sample{sample("R", "PAIRED", "public", nil, count(40), "R_1.fastq.gz", "R_2.fastq.gz")}}, r1)
	encoded, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	out := bytes.NewBuffer(encoded)
	var doc struct {
		Source struct {
			ID        string `json:"id"`
			Accession string `json:"accession"`
			Runs      []struct {
				Accession string `json:"accession"`
				Declared  struct {
					ReadCount *int64 `json:"read_count"`
					BaseCount *int64 `json:"base_count"`
				} `json:"declared"`
				Files []struct {
					Name  string  `json:"name"`
					Local *string `json:"local"`
				} `json:"files"`
				Assertions []model.Assertion `json:"assertions"`
			} `json:"runs"`
		} `json:"source"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	run := doc.Source.Runs[0]
	if doc.Source.ID != "ena" || run.Accession != "R" || run.Declared.ReadCount != nil || *run.Declared.BaseCount != 40 {
		t.Fatalf("doc %+v", doc)
	}
	if run.Files[0].Local == nil || run.Files[1].Local != nil || run.Files[1].Name != "R_2.fastq.gz" {
		t.Fatalf("files %+v", run.Files)
	}
}

func withChecksum(s model.Sample, j int, data []byte) model.Sample {
	sum := md5.Sum(data)
	artifacts := append([]model.Artifact(nil), s.Artifacts...)
	artifacts[j].MD5, artifacts[j].Size = hex.EncodeToString(sum[:]), int64(len(data))
	s.Artifacts = artifacts
	return s
}

func mates(t *testing.T) ([]byte, []byte) {
	t.Helper()
	r1 := reads("SRR1", 10, 50)
	r2 := bytes.ReplaceAll(r1, []byte("AAAAA"), []byte("CCCCC"))
	return gzipped(t, r1), gzipped(t, r2)
}

func pairedRun(r1, r2 []byte) model.Sample {
	s := sample("SRR1", "PAIRED", "public", count(10), count(1000), "SRR1_1.fastq.gz", "SRR1_2.fastq.gz")
	return withChecksum(withChecksum(s, 0, r1), 1, r2)
}

func TestSourceRenamedFilesAreLinkedByMD5(t *testing.T) {
	dir := t.TempDir()
	d1, d2 := mates(t)
	one := write(t, dir, "sample1_R1.fastq.gz", d1)
	two := write(t, dir, "sample1_R2.fastq.gz", d2)
	rep := against(t, Declaration{Samples: []model.Sample{pairedRun(d1, d2)}}, one, two)
	if code := rep.ExitCode(false); code != ExitOK {
		t.Fatalf("exit %d, failures %v", code, sourceFailures(rep))
	}
	for j, f := range rep.Source.Runs[0].Files {
		if f.Local == nil || f.LinkedBy != LinkedByMD5 {
			t.Fatalf("file %d: %+v", j, f)
		}
	}
	for _, f := range rep.Files {
		if has(failed(f), diagnoses.SourceFileUnlisted) {
			t.Fatalf("%s must not be unlisted", f.Name)
		}
	}
	if a := runAssertion(t, rep, 0, diagnoses.CountMismatch); !a.Passed {
		t.Fatalf("the linked files must enter the comparison: %+v", a)
	}
	linked := rep.Source.Runs[0].Files[0]
	if linked.LinkedBy != LinkedByMD5 || linked.Local == nil || filepath.Base(linked.Local.String()) != "sample1_R1.fastq.gz" || linked.Name.String() != "SRR1_1.fastq.gz" {
		t.Errorf("the renamed file must be linked by content: %+v", linked)
	}
	encoded, err := json.Marshal(rep)
	if err != nil || !strings.Contains(string(encoded), `"linked_by":"md5"`) {
		t.Fatalf("json lacks linked_by: %v\n%s", err, encoded)
	}
}

func TestSourceRenamedAndShortenedFileIsNotLinked(t *testing.T) {
	dir := t.TempDir()
	d1, d2 := mates(t)
	one := write(t, dir, "sample1_R1.fastq.gz", d1)
	shortened := write(t, dir, "sample1_R2.fastq.gz", gzipped(t, reads("SRR1", 10, 1)))
	rep := against(t, Declaration{Samples: []model.Sample{pairedRun(d1, d2)}}, one, shortened)
	if code := rep.ExitCode(false); code != ExitIncomplete {
		t.Fatalf("exit %d, want 3", code)
	}
	if m := runAssertion(t, rep, 0, diagnoses.SourceFileMissing); m.Declared != "SRR1_2.fastq.gz" {
		t.Fatalf("missing %+v", m)
	}
	if f := rep.Source.Runs[0].Files[1]; f.Local != nil || f.LinkedBy != "" {
		t.Fatalf("a different content must not be linked: %+v", f)
	}
	if !has(failed(rep.Files[1]), diagnoses.SourceFileUnlisted) {
		t.Fatalf("the shortened file: %s %v", rep.Files[1].Status, failed(rep.Files[1]))
	}
	if bc := runAssertion(t, rep, 0, diagnoses.CountMismatch); !bc.Skipped || bc.SkippedBy != diagnoses.SourceFileMissing {
		t.Fatalf("the run must not be judged complete: %+v", bc)
	}
}

func TestSourceTwoFilesWithTheContentOfOneDeclaredFileAreAmbiguous(t *testing.T) {
	dir := t.TempDir()
	data := gzipped(t, reads("R", 5, 4))
	a := write(t, mkdir(t, filepath.Join(dir, "a")), "x.fastq.gz", data)
	b := write(t, mkdir(t, filepath.Join(dir, "b")), "y.fastq.gz", data)
	s := withChecksum(sample("R", "SINGLE", "public", count(5), count(20), "R.fastq.gz"), 0, data)
	rep := against(t, Declaration{Samples: []model.Sample{s}}, a, b)
	for _, f := range rep.Files {
		if !has(failed(f), diagnoses.SourceFileAmbiguous) {
			t.Fatalf("%s: %v", f.Name, failed(f))
		}
	}
	if rep.Source.Runs[0].Files[0].Local != nil || rep.ExitCode(false) != ExitIncomplete {
		t.Fatalf("files %+v exit %d", rep.Source.Runs[0].Files, rep.ExitCode(false))
	}
	if bc := runAssertion(t, rep, 0, diagnoses.CountMismatch); !bc.Skipped || bc.SkippedBy != diagnoses.SourceFileAmbiguous {
		t.Fatalf("base count %+v", bc)
	}
}

func TestSourceOneFileWithTheContentOfTwoDeclaredFilesIsAmbiguous(t *testing.T) {
	dir := t.TempDir()
	data := gzipped(t, reads("R", 5, 4))
	local := write(t, dir, "x.fastq.gz", data)
	s := sample("R", "PAIRED", "public", count(5), count(40), "R_1.fastq.gz", "R_2.fastq.gz")
	s = withChecksum(withChecksum(s, 0, data), 1, data)
	rep := against(t, Declaration{Samples: []model.Sample{s}}, local)
	if !has(failed(rep.Files[0]), diagnoses.SourceFileAmbiguous) || rep.ExitCode(false) != ExitIncomplete {
		t.Fatalf("%v exit %d", failed(rep.Files[0]), rep.ExitCode(false))
	}
	for _, f := range rep.Source.Runs[0].Files {
		if f.Local != nil {
			t.Fatalf("one file must not stand for two: %+v", rep.Source.Runs[0].Files)
		}
	}
}

func TestSourceTheNameWinsOverAContentMatch(t *testing.T) {
	dir := t.TempDir()
	data := gzipped(t, reads("R", 5, 4))
	exact := write(t, dir, "R.fastq.gz", data)
	renamed := write(t, dir, "copy.fastq.gz", data)
	s := withChecksum(sample("R", "SINGLE", "public", count(5), count(20), "R.fastq.gz"), 0, data)
	rep := against(t, Declaration{Samples: []model.Sample{s}}, renamed, exact)
	f := rep.Source.Runs[0].Files[0]
	if f.Local == nil || filepath.Base(f.Local.String()) != "R.fastq.gz" || f.LinkedBy != LinkedByName {
		t.Fatalf("the exact name must stand for the declared file: %+v", f)
	}
	if !has(failed(rep.Files[0]), diagnoses.SourceFileUnlisted) || rep.ExitCode(false) != ExitOK {
		t.Fatalf("the renamed copy: %v exit %d", failed(rep.Files[0]), rep.ExitCode(false))
	}
}

func TestSourceRenamedFilesPairByTheDeclaredName(t *testing.T) {
	dir := t.TempDir()
	r1, r2 := mates(t)
	one := write(t, dir, "alpha.fastq.gz", r1)
	two := write(t, dir, "beta.fastq.gz", r2)
	s := pairedRun(r1, r2)
	s.Layout = "SINGLE"
	rep := against(t, Declaration{Samples: []model.Sample{s}}, one, two)
	if a := runAssertion(t, rep, 0, diagnoses.LayoutMismatch); a.Passed || !strings.Contains(a.Observed, "SRR1_1.fastq.gz and SRR1_2.fastq.gz") {
		t.Fatalf("the pair must come from the declared names: %+v", a)
	}
}

func mkdir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}
