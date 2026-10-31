package verifier

import (
	"sync/atomic"
	"testing"

	"github.com/colligo-go/colligo/identity"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
)

func TestProgressCountsEveryByteOnceAndKnowsTheTotalBeforeReading(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.fastq", fastqText("A", 300, "ACGTACGT"))
	b := write(t, dir, "b.fastq", fastqText("B", 500, "ACGTACGT"))
	var firstTotal atomic.Int64
	progress := NewProgress(func(done, total int64) {
		firstTotal.CompareAndSwap(0, total)
		if done > total {
			t.Errorf("done %d passed total %d", done, total)
		}
	})
	rep := run(t, Options{Progress: progress}, a, b)
	want := rep.Files[0].Size + rep.Files[1].Size
	if progress.Total() != want || progress.Done() != want || firstTotal.Load() != want {
		t.Fatalf("total %d done %d first total %d, want %d for all three", progress.Total(), progress.Done(), firstTotal.Load(), want)
	}
	for _, f := range rep.Files {
		if f.Status != StatusVerified {
			t.Errorf("counting changed what is checked: %s %s", f.Name, f.Status)
		}
	}
}

func TestCountingNeverChangesTheResult(t *testing.T) {
	dir := t.TempDir()
	n := write(t, dir, "a.fastq", fastqText("A", 300, "ACGTACGT"))
	plain := run(t, Options{}, n)
	counted := run(t, Options{Progress: NewProgress(nil)}, n)
	p, c := plain.Files[0], counted.Files[0]
	if p.BLAKE3 != c.BLAKE3 || p.MD5 != c.MD5 || p.SHA256 != c.SHA256 || p.Size != c.Size || len(p.Assertions) != len(c.Assertions) {
		t.Fatalf("plain %+v counted %+v", p, c)
	}
}

func TestANilProgressIsHarmless(t *testing.T) {
	var progress *Progress
	progress.advance(10)
	progress.expect(nil)
	if progress.Total() != 0 || progress.Done() != 0 {
		t.Fatal("a nil progress counted something")
	}
}

func TestQuickReadDoesNotCountProgress(t *testing.T) {
	dir := t.TempDir()
	n := write(t, dir, "a.fastq", fastqText("A", 300, "ACGTACGT"))
	progress := NewProgress(nil)
	run(t, Options{Quick: true, Progress: progress}, n)
	if progress.Total() != 0 {
		t.Errorf("a quick check reads a fraction, so a total of %d would mislead", progress.Total())
	}
}

func TestIdentifyCountsItsReadToo(t *testing.T) {
	dir := t.TempDir()
	n := write(t, dir, "known.fa", []byte(identifyText))
	progress := NewProgress(nil)
	report, err := IdentifyAll(t.Context(), []secret.Name{n}, identity.Table{}, WithProgress(progress))
	if err != nil {
		t.Fatal(err)
	}
	if progress.Total() != report.Files[0].Size || progress.Done() != progress.Total() || progress.Total() == 0 {
		t.Fatalf("total %d done %d size %d", progress.Total(), progress.Done(), report.Files[0].Size)
	}
}

func TestReportCarriesWhatTheRenderersNeed(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "R_1.fastq", reads("R", 5, 4))
	rep := against(t, Declaration{Samples: []model.Sample{sample("R", "PAIRED", "public", nil, count(20), "R_1.fastq.gz")}}, r1)
	if rep.Elapsed <= 0 {
		t.Errorf("elapsed %v", rep.Elapsed)
	}
	reads, bases, ok := rep.Files[0].Records()
	if !ok || reads != 5 || bases != 20 {
		t.Errorf("records %d %d %v", reads, bases, ok)
	}
	if totals := rep.Source.Runs[0].Measured; totals == nil || totals.Reads != 5 || totals.Bases != 20 {
		t.Errorf("measured %+v", totals)
	}
}

func TestRunTotalsAreAbsentWhenTheComparisonCouldNotCountThem(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "R_1.fastq", reads("R", 5, 4))
	missing := against(t, Declaration{Samples: []model.Sample{sample("R", "PAIRED", "public", nil, count(40), "R_1.fastq.gz", "R_2.fastq.gz")}}, r1)
	if got := missing.Source.Runs[0].Measured; got != nil {
		t.Errorf("a run with a missing file must not report totals: %+v", got)
	}
	none := against(t, Declaration{Samples: []model.Sample{sample("R", "PAIRED", "suppressed", nil, count(40))}})
	if got := none.Source.Runs[0].Measured; got != nil {
		t.Errorf("a run that lists no file must not report totals: %+v", got)
	}
	noRecords := []*FileReport{{Name: secret.NewName("/data/R_2.fastq"), Status: StatusVerified}}
	_, totals := contentAssertions(sample("R", "SINGLE", "public", nil, count(40), "R_2.fastq.gz"), noRecords, "")
	if totals != nil {
		t.Errorf("a file with no records to count must not report totals: %+v", totals)
	}
}

func TestSamplesheetReportsSaysSo(t *testing.T) {
	dir := t.TempDir()
	writeMates(t, dir, "a", "AAA", 5)
	rep := checkSheet(t, dir, "sample,fastq_1,fastq_2\nA,a_R1.fastq,a_R2.fastq\n")
	if !rep.FromSamplesheet || rep.Elapsed <= 0 {
		t.Errorf("samplesheet %v elapsed %v", rep.FromSamplesheet, rep.Elapsed)
	}
	if plain := run(t, Options{}, write(t, dir, "x.fastq", fastqText("X", 3, "ACGT"))); plain.FromSamplesheet {
		t.Error("a plain check claims to come from a samplesheet")
	}
}
