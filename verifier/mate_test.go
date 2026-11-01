package verifier

import (
	"path/filepath"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/model"
)

func collapsedFiles(rep Report) []string {
	var out []string
	for _, f := range rep.Files {
		if has(failed(f), diagnoses.MateCollapsed) {
			out = append(out, filepath.Base(f.Name.String()))
		}
	}
	return out
}

func checkMates(t *testing.T, first, second int) Report {
	t.Helper()
	dir := t.TempDir()
	return run(t, Options{},
		write(t, dir, "SRR1_1.fastq", reads("SRR1", 20, first)),
		write(t, dir, "SRR1_2.fastq", reads("SRR1", 20, second)))
}

func TestAMateOfOneBaseAgainstOneOfOneHundredTwentyFiveIsFlaggedOnTheShortOne(t *testing.T) {
	rep := checkMates(t, 125, 1)
	if got := collapsedFiles(rep); len(got) != 1 || got[0] != "SRR1_2.fastq" {
		t.Fatalf("flagged %v, want only SRR1_2.fastq", got)
	}
	short := rep.Files[1]
	if short.Status != StatusVerified {
		t.Errorf("status %s: an advisory must not stop a verified file", short.Status)
	}
	if short.Worst() != model.Advisory {
		t.Errorf("severity %s, want advisory", short.Worst())
	}
	if code := rep.ExitCode(false); code != ExitOK {
		t.Errorf("exit %d: the finding is never blocking", code)
	}
	mirrored := checkMates(t, 1, 125)
	if got := collapsedFiles(mirrored); len(got) != 1 || got[0] != "SRR1_1.fastq" {
		t.Errorf("mirrored case flagged %v", got)
	}
}

func TestMateLengthsOfRealDesignsAreNeverFlagged(t *testing.T) {
	measured := map[string][2]int{
		"10x 3' v2, mean 105 against 17":    {105, 17},
		"10x 3' v3, 26 against 98":          {26, 98},
		"10x with hashtag, 26 against 90":   {26, 90},
		"10x, 36 against 88":                {36, 88},
		"single-cell, 86 against 36":        {86, 36},
		"trimmed pair, 280 against 210":     {280, 210},
		"equal 150 against 150":             {150, 150},
		"barcode mate of 8 against 150":     {150, 8},
		"index mate of 6 against 150":       {150, 6},
		"two bases against 20":              {20, 2},
		"three bases against 50 (ratio 6%)": {50, 3},
		"four bases against 150":            {150, 4},
	}
	for name, lengths := range measured {
		if got := collapsedFiles(checkMates(t, lengths[0], lengths[1])); len(got) != 0 {
			t.Errorf("%s: flagged %v", name, got)
		}
	}
}

func TestBothLimitsMustHoldToFlagAMate(t *testing.T) {
	flagged := map[string][2]int{
		"three bases against 100": {100, 3},
		"two bases against 100":   {100, 2},
		"one base against 21":     {21, 1},
	}
	for name, lengths := range flagged {
		if got := collapsedFiles(checkMates(t, lengths[0], lengths[1])); len(got) != 1 {
			t.Errorf("%s: flagged %v, want one file", name, got)
		}
	}
	notFlagged := map[string][2]int{
		"under four but not under 5% (3 against 50)": {50, 3},
		"one base against 19 (5.3%)":                 {19, 1},
		"four bases against 1000":                    {1000, 4},
		"eight bases against 500 (1.6%)":             {500, 8},
	}
	for name, lengths := range notFlagged {
		if got := collapsedFiles(checkMates(t, lengths[0], lengths[1])); len(got) != 0 {
			t.Errorf("%s: flagged %v", name, got)
		}
	}
}

func TestMatesWithDifferentReadCountsAreBlockingAndNotAlsoFlaggedAsCollapsed(t *testing.T) {
	dir := t.TempDir()
	rep := run(t, Options{},
		write(t, dir, "SRR1_1.fastq", reads("SRR1", 20, 125)),
		write(t, dir, "SRR1_2.fastq", reads("SRR1", 7, 1)))
	if got := collapsedFiles(rep); len(got) != 0 {
		t.Errorf("flagged %v on a pair whose read counts differ", got)
	}
	if code := rep.ExitCode(false); code != ExitIncomplete {
		t.Errorf("exit %d", code)
	}
}

func TestACollapsedMateIsFlaggedInASamplesheetToo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a_R1.fastq", reads("A", 20, 125))
	write(t, dir, "a_R2.fastq", reads("A", 20, 1))
	rep := checkSheet(t, dir, "sample,fastq_1,fastq_2,strandedness\nA,a_R1.fastq,a_R2.fastq,auto\n")
	if got := collapsedFiles(rep); len(got) != 1 || got[0] != "a_R2.fastq" {
		t.Fatalf("flagged %v", got)
	}
}

func declaredBases(t *testing.T, baseCount *int64) Report {
	t.Helper()
	dir := t.TempDir()
	one := write(t, dir, "SRR1_1.fastq", reads("SRR1", 20, 125))
	two := write(t, dir, "SRR1_2.fastq", reads("SRR1", 20, 1))
	decl := Declaration{Samples: []model.Sample{sample("SRR1", "PAIRED", "public", count(20), baseCount, "SRR1_1.fastq.gz", "SRR1_2.fastq.gz")}}
	return against(t, decl, one, two)
}

func TestWhenTheBasesAreComparedWithTheSourceTheHeuristicStepsAside(t *testing.T) {
	for label, declared := range map[string]int64{"the bases fail": 5000, "the bases pass": 2520} {
		rep := declaredBases(t, count(declared))
		if got := collapsedFiles(rep); len(got) != 0 {
			t.Errorf("%s: flagged %v next to the precise comparison", label, got)
		}
	}
	if code := declaredBases(t, count(5000)).ExitCode(false); code != ExitIncomplete {
		t.Errorf("exit %d: the bases comparison must still be blocking", code)
	}
}

func TestWhenTheSourceDeclaresNoBasesTheHeuristicStillSpeaks(t *testing.T) {
	rep := declaredBases(t, nil)
	if got := collapsedFiles(rep); len(got) != 1 || got[0] != "SRR1_2.fastq" {
		t.Fatalf("flagged %v, want only SRR1_2.fastq", got)
	}
}

func TestTheLimitsAreTheMeasuredOnes(t *testing.T) {
	if collapsedMateBases != 4 || collapsedMateRatio != 0.05 {
		t.Fatalf("limits %d and %v: change docs/RELIABILITY.md M15 with them", collapsedMateBases, collapsedMateRatio)
	}
}
