package verifier

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/samplesheet"
	"github.com/colligo-go/colligo/secret"
)

func parseSheet(t *testing.T, dir, text string) samplesheet.Sheet {
	t.Helper()
	sheet, err := samplesheet.Parse(strings.NewReader(text), secret.NewName(filepath.Join(dir, "samples.csv")))
	if err != nil {
		t.Fatal(err)
	}
	return sheet
}

func checkSheet(t *testing.T, dir, text string) Report {
	t.Helper()
	rep, err := CheckSamplesheet(context.Background(), parseSheet(t, dir, text), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func failures(rep Report, id string) []model.Assertion {
	var out []model.Assertion
	for _, f := range rep.Files {
		for _, a := range f.Assertions {
			if a.ID == id && !a.Passed && !a.Skipped {
				out = append(out, a)
			}
		}
	}
	return out
}

func distinct(as []model.Assertion) []string {
	var out []string
	for _, a := range as {
		if !slices.Contains(out, a.Observation) {
			out = append(out, a.Observation)
		}
	}
	return out
}

func writeMates(t *testing.T, dir, stem, readPrefix string, reads int) {
	t.Helper()
	write(t, dir, stem+"_R1.fastq", fastqText(readPrefix, reads, "ACGTACGT"))
	write(t, dir, stem+"_R2.fastq", fastqText(readPrefix, reads, "TTGGCCAA"))
}

func TestSamplesheetWithRepeatedLanesIsVerified(t *testing.T) {
	dir := t.TempDir()
	writeMates(t, dir, "wt_l1", "WT1", 10)
	writeMates(t, dir, "wt_l2", "WT2", 12)
	writeMates(t, dir, "ko_l1", "KO1", 8)
	rep := checkSheet(t, dir, "sample,fastq_1,fastq_2,strandedness\n"+
		"WT,wt_l1_R1.fastq,wt_l1_R2.fastq,auto\n"+
		"WT,wt_l2_R1.fastq,wt_l2_R2.fastq,auto\n"+
		"KO,ko_l1_R1.fastq,ko_l1_R2.fastq,auto\n")
	if code := rep.ExitCode(false); code != ExitOK {
		t.Fatalf("lanes of one sample are legitimate, exit %d", code)
	}
	if len(rep.Files) != 6 {
		t.Fatalf("%d files reported, want 6", len(rep.Files))
	}
	for _, f := range rep.Files {
		if f.Status != StatusVerified || len(failed(f)) != 0 {
			t.Fatalf("%s: %s %v", f.Name, f.Status, failed(f))
		}
		if len(declaredPairResults(f)) != 2 {
			t.Fatalf("%s: the declared pair was not evaluated", f.Name)
		}
	}
}

func declaredPairResults(f *FileReport) []string {
	var ids []string
	for _, a := range f.Assertions {
		if (a.ID == diagnoses.PairCount || a.ID == diagnoses.PairIDs) && a.Passed {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

func TestSamplesheetWithMateSwappedBetweenTwoSamplesIsBlockingAndNamesTheLines(t *testing.T) {
	dir := t.TempDir()
	writeMates(t, dir, "a", "AAA", 10)
	writeMates(t, dir, "b", "BBB", 10)
	rep := checkSheet(t, dir, "sample,fastq_1,fastq_2\n"+
		"A,a_R1.fastq,b_R2.fastq\n"+
		"B,b_R1.fastq,a_R2.fastq\n")
	if code := rep.ExitCode(false); code != ExitIncomplete {
		t.Fatalf("exit %d, want %d", code, ExitIncomplete)
	}
	swapped := failures(rep, diagnoses.SheetMatesSwapped)
	if len(swapped) == 0 || swapped[0].Declared != "2" || swapped[0].Observed != "3" ||
		!strings.Contains(swapped[0].Observation, "lines 2 and 3") || swapped[0].Severity != model.Blocking {
		t.Fatalf("swap not named by lines: %+v", swapped)
	}
	ids := failures(rep, diagnoses.PairIDs)
	if len(ids) == 0 {
		t.Fatal("the declared pairs must fail pair_ids")
	}
	for _, line := range []string{"line 2", "line 3"} {
		found := false
		for _, a := range ids {
			found = found || strings.Contains(a.Declared, line)
		}
		if !found {
			t.Errorf("no pair_ids assertion names %s", line)
		}
	}
	for _, f := range rep.Files {
		if f.Status != StatusInvalid {
			t.Errorf("%s: %s, want invalid", f.Name, f.Status)
		}
	}
}

func TestSamplesheetMatesWithDifferentCountsFailPairCountOnTheirLine(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a_R1.fastq", fastqText("AAA", 10, "ACGT"))
	write(t, dir, "a_R2.fastq", fastqText("AAA", 9, "TTGG"))
	rep := checkSheet(t, dir, "sample,fastq_1,fastq_2\nA,a_R1.fastq,a_R2.fastq\n")
	counts := failures(rep, diagnoses.PairCount)
	if len(counts) == 0 || !strings.Contains(counts[0].Declared, "line 2") || rep.ExitCode(false) != ExitIncomplete {
		t.Fatalf("%+v, exit %d", counts, rep.ExitCode(false))
	}
}

func TestSamplesheetDeclaresTheMatesNotTheNames(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "x.fastq", fastqText("XXX", 5, "ACGT"))
	write(t, dir, "y.fastq", fastqText("XXX", 5, "TTGG"))
	write(t, dir, "lonely_R1.fastq", fastqText("LLL", 5, "ACGT"))
	rep := checkSheet(t, dir, "sample,fastq_1,fastq_2\nA,x.fastq,y.fastq\nB,lonely_R1.fastq,\n")
	if code := rep.ExitCode(false); code != ExitOK {
		t.Fatalf("exit %d: names without a mate suffix pair by declaration, and a single-end row is not a missing mate", code)
	}
	if got := failures(rep, diagnoses.PairMissing); len(got) != 0 {
		t.Fatalf("pair_missing fired on a single-end row: %+v", got)
	}
}

func TestSamplesheetMissingFileNamesTheLineAndKeepsChecking(t *testing.T) {
	dir := t.TempDir()
	writeMates(t, dir, "a", "AAA", 5)
	rep := checkSheet(t, dir, "sample,fastq_1,fastq_2\nA,a_R1.fastq,a_R2.fastq\nB,gone_R1.fastq,gone_R2.fastq\n")
	read := failures(rep, diagnoses.ReadFailed)
	if len(read) != 2 {
		t.Fatalf("%d read_failed, want 2 (both mates are absent)", len(read))
	}
	for _, a := range read {
		if !strings.Contains(a.Observation, "samplesheet line 3") || a.Declared != "samplesheet line 3" {
			t.Fatalf("the missing file does not say where it is listed: %+v", a)
		}
	}
	if code := rep.ExitCode(false); code != ExitFatal {
		t.Fatalf("exit %d, want %d", code, ExitFatal)
	}
	if rep.Files[0].Status != StatusVerified {
		t.Fatalf("the good sample was not checked: %s", rep.Files[0].Status)
	}
}

func TestSamplesheetRowRepeatedWithTheSameFilesIsBlocking(t *testing.T) {
	dir := t.TempDir()
	writeMates(t, dir, "a", "AAA", 6)
	writeMates(t, dir, "b", "BBB", 6)
	rep := checkSheet(t, dir, "sample,fastq_1,fastq_2\n"+
		"A,a_R1.fastq,a_R2.fastq\n"+
		"B,b_R1.fastq,b_R2.fastq\n"+
		"A,a_R1.fastq,a_R2.fastq\n")
	dup := failures(rep, diagnoses.SheetDuplicateRow)
	if len(dup) != 1 || !strings.Contains(dup[0].Observation, "lines 2 and 4 repeat sample A") {
		t.Fatalf("%+v", dup)
	}
	if got := failures(rep, diagnoses.SheetSharedFile); len(got) != 0 {
		t.Fatalf("a repeated row is reported once, not also as shared files: %+v", got)
	}
	if rep.ExitCode(false) != ExitIncomplete {
		t.Fatalf("exit %d", rep.ExitCode(false))
	}
	if len(rep.Files) != 4 {
		t.Fatalf("%d files, want 4: each file appears once however many lines name it", len(rep.Files))
	}
}

func TestSamplesheetSameFileInTwoSamplesIsBlocking(t *testing.T) {
	dir := t.TempDir()
	writeMates(t, dir, "a", "AAA", 6)
	writeMates(t, dir, "b", "BBB", 6)
	rep := checkSheet(t, dir, "sample,fastq_1,fastq_2\n"+
		"A,a_R1.fastq,a_R2.fastq\n"+
		"B,b_R1.fastq,a_R2.fastq\n")
	shared := failures(rep, diagnoses.SheetSharedFile)
	if got := distinct(shared); len(got) != 1 || !strings.Contains(got[0], "lines 2 and 3") || rep.ExitCode(false) != ExitIncomplete {
		t.Fatalf("%q exit %d", got, rep.ExitCode(false))
	}
}

func TestSamplesheetCopyUnderAnotherNameIsTheSameFile(t *testing.T) {
	dir := t.TempDir()
	writeMates(t, dir, "a", "AAA", 6)
	write(t, dir, "copy_R1.fastq", fastqText("AAA", 6, "ACGTACGT"))
	write(t, dir, "copy_R2.fastq", fastqText("AAA", 6, "TTGGCCAA"))
	rep := checkSheet(t, dir, "sample,fastq_1,fastq_2\nA,a_R1.fastq,a_R2.fastq\nB,copy_R1.fastq,copy_R2.fastq\n")
	if got := distinct(failures(rep, diagnoses.SheetSharedFile)); len(got) != 2 || rep.ExitCode(false) != ExitIncomplete {
		t.Fatalf("content identity must catch a copy of each mate: %q exit %d", got, rep.ExitCode(false))
	}
}

func TestSamplesheetSameFileAsBothMatesIsBlocking(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a_R1.fastq", fastqText("AAA", 6, "ACGT"))
	rep := checkSheet(t, dir, "sample,fastq_1,fastq_2\nA,a_R1.fastq,a_R1.fastq\n")
	shared := failures(rep, diagnoses.SheetSharedFile)
	if got := distinct(shared); len(got) != 1 || !strings.Contains(got[0], "line 2 (fastq_1 and fastq_2)") {
		t.Fatalf("%q", got)
	}
	if len(rep.Files) != 1 {
		t.Fatalf("%d files, want 1", len(rep.Files))
	}
}

func TestSamplesheetRemoteLocationIsBlockingAndRedacted(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a_R1.fastq", fastqText("AAA", 6, "ACGT"))
	rep := checkSheet(t, dir, "sample,fastq_1,fastq_2\nA,a_R1.fastq,https://host.example/a_R2.fastq.gz?token=SECRET\nB,s3://bucket/b.fq.gz,\n")
	remote := failures(rep, diagnoses.SheetRemotePath)
	if len(remote) != 2 || !strings.Contains(remote[0].Observation, "line 2") || !strings.Contains(remote[1].Observation, "line 3") {
		t.Fatalf("%+v", remote)
	}
	if rep.ExitCode(false) != ExitIncomplete || rep.ExitCode(true) != ExitOK {
		t.Fatalf("exit %d strict, %d permissive", rep.ExitCode(false), rep.ExitCode(true))
	}
	var text strings.Builder
	rep.WriteText(&text)
	if strings.Contains(text.String(), "SECRET") {
		t.Fatalf("the query string leaked into the report:\n%s", text.String())
	}
}

func TestSamplesheetReadsEachFileExactlyOnce(t *testing.T) {
	dir := t.TempDir()
	var total int64
	for stem, prefix := range map[string]string{"a": "AAA", "b": "BBB"} {
		data := fastqText(prefix, 40_000, "ACGTACGTACGT")
		write(t, dir, stem+"_R1.fastq", data)
		other := fastqText(prefix, 40_000, "TTGGCCAATTGG")
		write(t, dir, stem+"_R2.fastq", other)
		total += int64(len(data) + len(other))
	}
	sheet := parseSheet(t, dir, "sample,fastq_1,fastq_2\n"+
		"A,a_R1.fastq,a_R2.fastq\nA,a_R1.fastq,a_R2.fastq\nB,b_R1.fastq,b_R2.fastq\nB,a_R1.fastq,b_R2.fastq\n")
	got := leastBytesRead(t, total, func() {
		if _, err := CheckSamplesheet(context.Background(), sheet, Options{Jobs: 4}); err != nil {
			t.Fatal(err)
		}
	})
	if got < total || got > total+schedulerWakeups {
		t.Fatalf("%d bytes read for %d bytes of distinct files listed on 4 lines", got, total)
	}
}

func TestSamplesheetDoesNotWriteAnythingNextToTheFiles(t *testing.T) {
	dir := t.TempDir()
	writeMates(t, dir, "a", "AAA", 5)
	checkSheet(t, dir, "sample,fastq_1,fastq_2\nA,a_R1.fastq,a_R2.fastq\n")
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("%v %v", entries, err)
	}
}
