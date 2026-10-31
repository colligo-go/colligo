package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/verifier"
)

func mate2(prefix string, reads int) []byte {
	return bytes.ReplaceAll(fastq(prefix, reads), []byte("ACGTACGTAC"), []byte("TTGGCCAATT"))
}

func putMates(t *testing.T, dir, stem, prefix string, reads int, suffix string, compress bool) {
	t.Helper()
	r1, r2 := fastq(prefix, reads), mate2(prefix, reads)
	if compress {
		r1, r2 = gz(r1), gz(r2)
	}
	put(t, dir, stem+"_R1"+suffix, r1)
	put(t, dir, stem+"_R2"+suffix, r2)
}

func sheetCommand(t *testing.T, dir, text string, extra ...string) (int, string, string) {
	t.Helper()
	sheet := put(t, dir, "samples.csv", []byte(text))
	return invoke(append([]string{"check", "--samplesheet", sheet}, extra...)...)
}

func TestSamplesheetWithSwappedMateIsExit3AndNamesTheLines(t *testing.T) {
	dir := t.TempDir()
	putMates(t, dir, "a", "AAA", 10, ".fastq.gz", true)
	putMates(t, dir, "b", "BBB", 10, ".fastq.gz", true)
	code, out, _ := sheetCommand(t, dir, "sample,fastq_1,fastq_2,strandedness\n"+
		"A,a_R1.fastq.gz,b_R2.fastq.gz,auto\n"+
		"B,b_R1.fastq.gz,a_R2.fastq.gz,auto\n")
	if code != verifier.ExitIncomplete {
		t.Fatalf("exit %d, want 3\n%s", code, out)
	}
	for _, want := range []string{"BLOCKING", "sheet_mates_swapped", "lines 2 and 3", "pair_ids"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
}

func TestSamplesheetWithRepeatedLanesIsExit0AndRelativePathsFollowTheSheet(t *testing.T) {
	dir := t.TempDir()
	putMates(t, dir, "wt_l1", "W1", 10, ".fastq.gz", true)
	putMates(t, dir, "wt_l2", "W2", 10, ".fastq.gz", true)
	putMates(t, dir, "ko_l1", "K1", 10, ".fastq.gz", true)
	code, out, errText := sheetCommand(t, dir, "sample,fastq_1,fastq_2,strandedness\n"+
		"WT,wt_l1_R1.fastq.gz,wt_l1_R2.fastq.gz,auto\n"+
		"WT,wt_l2_R1.fastq.gz,wt_l2_R2.fastq.gz,auto\n"+
		"KO,ko_l1_R1.fastq.gz,ko_l1_R2.fastq.gz,auto\n")
	if code != verifier.ExitOK {
		t.Fatalf("exit %d\n%s%s", code, out, errText)
	}
	if !strings.Contains(out, "6 file(s): 6 verified") {
		t.Fatalf("every file must be verified once:\n%s", out)
	}
}

func TestSamplesheetJSONKeepsSchemaVersion1(t *testing.T) {
	dir := t.TempDir()
	putMates(t, dir, "a", "AAA", 5, ".fastq", false)
	putMates(t, dir, "b", "BBB", 5, ".fastq", false)
	code, out, _ := sheetCommand(t, dir, "sample,fastq_1,fastq_2\nA,a_R1.fastq,b_R2.fastq\nB,b_R1.fastq,a_R2.fastq\n", "--json")
	var doc struct {
		SchemaVersion int `json:"schema_version"`
		ExitCode      int `json:"exit_code"`
		Files         []struct {
			Assertions []struct {
				ID string `json:"id"`
			} `json:"assertions"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if code != verifier.ExitIncomplete || doc.SchemaVersion != 1 || doc.ExitCode != verifier.ExitIncomplete || len(doc.Files) != 4 {
		t.Fatalf("exit %d, schema %d, exit_code %d, %d files", code, doc.SchemaVersion, doc.ExitCode, len(doc.Files))
	}
}

func TestSamplesheetMissingFileIsExit2AndNamesTheLine(t *testing.T) {
	dir := t.TempDir()
	putMates(t, dir, "a", "AAA", 5, ".fastq", false)
	code, out, _ := sheetCommand(t, dir, "sample,fastq_1,fastq_2\nA,a_R1.fastq,a_R2.fastq\nB,gone_R1.fastq,gone_R2.fastq\n")
	if code != verifier.ExitFatal || !strings.Contains(out, "read_failed") || !strings.Contains(out, "samplesheet line 3") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestSamplesheetRemoteLocationIsExit3AndPermissiveLowersItTo0(t *testing.T) {
	dir := t.TempDir()
	text := "sample,fastq_1,fastq_2\nA,s3://bucket/a_R1.fq.gz,s3://bucket/a_R2.fq.gz\n"
	code, out, _ := sheetCommand(t, dir, text)
	if code != verifier.ExitIncomplete || !strings.Contains(out, "sheet_remote_path") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if code, _, _ := sheetCommand(t, dir, text, "--permissive"); code != verifier.ExitOK {
		t.Fatalf("permissive exit %d", code)
	}
}

func TestSamplesheetUsageErrorsExit1BeforeAnyFileIsRead(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"missing column":    "sample,fastq_1\nA,a.fq\n",
		"unknown format":    "id,reads\nA,a.fq\n",
		"malformed csv":     "sample,fastq_1,fastq_2\nA,\"a.fq,b.fq\n",
		"short row":         "sample,fastq_1,fastq_2\nA,a.fq\n",
		"empty file":        "",
		"header only":       "sample,fastq_1,fastq_2\n",
		"bad strandedness":  "sample,fastq_1,strandedness\nA,a.fq,sideways\n",
		"empty fastq_1":     "sample,fastq_1,fastq_2\nA,,b.fq\n",
		"unsupported extra": "sample,fastq_1,fastq_2,replicate\nA,a.fq,b.fq,1\n",
	}
	for name, text := range cases {
		code, out, errText := sheetCommand(t, dir, text)
		if code != verifier.ExitUsage || out != "" || !strings.Contains(errText, "samplesheet") {
			t.Errorf("%s: exit %d stdout %q stderr %q", name, code, out, errText)
		}
	}
}

func TestSamplesheetOtherUsageErrors(t *testing.T) {
	dir := t.TempDir()
	sheet := put(t, dir, "samples.csv", []byte("sample,fastq_1,fastq_2\nA,a.fq,b.fq\n"))
	for _, args := range [][]string{
		{"check", "--samplesheet", dir + "/absent.csv"},
		{"check", "--samplesheet", sheet, "extra.fastq"},
		{"check", "--samplesheet", sheet, "--quick"},
		{"check", "--samplesheet", sheet, "--expected", "size=1"},
		{"check", "--samplesheet", sheet, "--format", "fastq"},
		{"check", "--samplesheet", sheet, "--recursive"},
		{"validate", "--samplesheet", sheet},
	} {
		if code, _, _ := invoke(args...); code != verifier.ExitUsage {
			t.Errorf("%v: exit %d, want 1", args[1:], code)
		}
	}
}
