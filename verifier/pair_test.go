package verifier

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/secret"
)

func fastqWithHeaders(headers ...string) []byte {
	var b bytes.Buffer
	for _, h := range headers {
		fmt.Fprintf(&b, "@%s\nACGTACGT\n+\nFFFFFFFF\n", h)
	}
	return b.Bytes()
}

func numbered(n int, suffix string) []string {
	headers := make([]string, n)
	for i := range headers {
		headers[i] = fmt.Sprintf("read%d%s", i+1, suffix)
	}
	return headers
}

func pairAssertions(rep *FileReport) []string {
	var ids []string
	for _, a := range rep.Assertions {
		if a.ID == diagnoses.PairCount || a.ID == diagnoses.PairIDs {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

func writeIn(t *testing.T, dir, sub, base string, data []byte) secret.Name {
	t.Helper()
	d := filepath.Join(dir, sub)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	return write(t, d, base, data)
}

func TestMatesInDifferentDirectoriesAreNotPaired(t *testing.T) {
	dir := t.TempDir()
	r1 := writeIn(t, dir, "a", "s_R1.fastq", fastqWithHeaders(numbered(10, "")...))
	r2 := writeIn(t, dir, "b", "s_R2.fastq", fastqWithHeaders(numbered(7, "")...))
	rep := run(t, Options{}, r1, r2)
	for _, f := range rep.Files {
		if got := pairAssertions(f); len(got) != 0 {
			t.Fatalf("%s: pair assertions %v for files in different directories", f.Name, got)
		}
		if f.Status != StatusVerified {
			t.Fatalf("%s: %s", f.Name, f.Status)
		}
	}
}

func TestMateWithTwoCopiesMakesNoPairClaim(t *testing.T) {
	dir := t.TempDir()
	reads := fastqWithHeaders(numbered(10, "")...)
	cases := map[string][]secret.Name{
		"duplicated R1": {
			write(t, dir, "d_R1.fastq", reads),
			write(t, dir, "d_R1.fastq.gz", gzipped(t, reads)),
			write(t, dir, "d_R2.fastq", fastqWithHeaders(numbered(7, "")...)),
		},
		"duplicated R1 listed last": {
			write(t, dir, "e_R1.fastq", reads),
			write(t, dir, "e_R2.fastq", fastqWithHeaders(numbered(7, "")...)),
			write(t, dir, "e_R1.fastq.gz", gzipped(t, reads)),
		},
		"three copies of R1": {
			write(t, dir, "f_R1.fastq", reads),
			write(t, dir, "f_R1.fastq.gz", gzipped(t, reads)),
			write(t, dir, "f_R1.fq", reads),
			write(t, dir, "f_R2.fastq", fastqWithHeaders(numbered(7, "")...)),
		},
	}
	for name, names := range cases {
		rep := run(t, Options{}, names...)
		for _, f := range rep.Files {
			if got := pairAssertions(f); len(got) != 0 {
				t.Errorf("%s: %s carries pair assertions %v", name, f.Name, got)
			}
		}
	}
}

func TestDifferentChunkNumbersAreNotPaired(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "S1_L001_R1_001.fastq", fastqWithHeaders(numbered(10, "")...))
	r2 := write(t, dir, "S1_L001_R2_002.fastq", fastqWithHeaders(numbered(7, "")...))
	rep := run(t, Options{}, r1, r2)
	for _, f := range rep.Files {
		if got := pairAssertions(f); len(got) != 0 {
			t.Fatalf("%s: pair assertions %v for different chunk numbers", f.Name, got)
		}
	}
}

func TestMateSuffixStylesInReadIDs(t *testing.T) {
	cases := []struct {
		name       string
		r1, r2     string
		wantFailed []string
	}{
		{"slash against slash", "/1", "/2", nil},
		{"slash against illumina comment", "/1", " 2:N:0:ACGT", nil},
		{"illumina comment against slash", " 1:N:0:ACGT", "/2", nil},
		{"illumina comment against illumina comment", " 1:N:0:ACGT", " 2:N:0:ACGT", nil},
		{"no suffix against slash", "", "/2", nil},
		{"dot against dot", ".1", ".2", []string{diagnoses.PairIDs}},
		{"slash against dot", "/1", ".2", []string{diagnoses.PairIDs}},
		{"dot against illumina comment", ".1", " 2:N:0:ACGT", []string{diagnoses.PairIDs}},
	}
	for _, c := range cases {
		dir := t.TempDir()
		r1 := write(t, dir, "m_R1.fastq", fastqWithHeaders(numbered(10, c.r1)...))
		r2 := write(t, dir, "m_R2.fastq", fastqWithHeaders(numbered(10, c.r2)...))
		rep := run(t, Options{}, r1, r2)
		got := failed(rep.Files[0])
		if fmt.Sprint(got) != fmt.Sprint(c.wantFailed) {
			t.Errorf("%s: failed %v, want %v", c.name, got, c.wantFailed)
		}
	}
}

func TestMateInGzipAndMateInPlainTextArePaired(t *testing.T) {
	dir := t.TempDir()
	reads := fastqWithHeaders(numbered(10, "")...)
	short := fastqWithHeaders(numbered(9, "")...)

	clean := run(t, Options{},
		write(t, dir, "ok_R1.fastq.gz", gzipped(t, reads)),
		write(t, dir, "ok_R2.fastq", reads))
	for _, f := range clean.Files {
		if f.Status != StatusVerified || len(pairAssertions(f)) != 2 {
			t.Fatalf("%s: %s, pair assertions %v", f.Name, f.Status, pairAssertions(f))
		}
	}

	broken := run(t, Options{},
		write(t, dir, "bad_R1.fastq.gz", gzipped(t, reads)),
		write(t, dir, "bad_R2.fastq", short))
	for _, f := range broken.Files {
		if !has(failed(f), diagnoses.PairCount) {
			t.Fatalf("%s: %v", f.Name, failed(f))
		}
	}
}

func TestSwappedMiddleReadsAreNotCaughtByThePairInvariant(t *testing.T) {
	dir := t.TempDir()
	in := numbered(100, "/1")
	swapped := numbered(100, "/2")
	swapped[40], swapped[41] = swapped[41], swapped[40]
	rep := run(t, Options{},
		write(t, dir, "w_R1.fastq", fastqWithHeaders(in...)),
		write(t, dir, "w_R2.fastq", fastqWithHeaders(swapped...)))
	for _, f := range rep.Files {
		if f.Status != StatusVerified || len(failed(f)) != 0 {
			t.Fatalf("%s: %s %v", f.Name, f.Status, failed(f))
		}
		if len(pairAssertions(f)) != 2 {
			t.Fatalf("%s: the pair was not evaluated: %v", f.Name, pairAssertions(f))
		}
	}
}

func missingMateOf(rep *FileReport) (string, bool) {
	for _, a := range rep.Assertions {
		if a.ID == diagnoses.PairMissing && !a.Passed {
			return a.Declared, true
		}
	}
	return "", false
}

func TestMateWithoutItsOtherMateIsAdvisoryAndNamesTheExpectedFile(t *testing.T) {
	dir := t.TempDir()
	reads := fastqWithHeaders(numbered(10, "")...)
	cases := []struct {
		lone, want string
	}{
		{"a_R1.fastq", "a_R2.fastq"},
		{"b_R2.fastq.gz", "b_R1.fastq.gz"},
		{"c_1.fq", "c_2.fq"},
		{"d.2.fastq", "d.1.fastq"},
		{"S1_L001_R1_001.fastq", "S1_L001_R2_001.fastq"},
	}
	for _, c := range cases {
		data := reads
		if strings.HasSuffix(c.lone, ".gz") {
			data = gzipped(t, reads)
		}
		rep := run(t, Options{}, write(t, dir, c.lone, data), write(t, dir, "unrelated-"+c.lone[:1]+".fastq", reads))
		got, ok := missingMateOf(rep.Files[0])
		if !ok || got != c.want {
			t.Errorf("%s: pair_missing %q (%v), want %q", c.lone, got, ok, c.want)
		}
		if rep.Files[0].Status != StatusVerified || rep.ExitCode(false) != ExitOK {
			t.Errorf("%s: pair_missing must stay advisory: %s", c.lone, rep.Files[0].Status)
		}
		if _, flagged := missingMateOf(rep.Files[1]); flagged {
			t.Errorf("a file without a mate suffix was flagged")
		}
	}
}

func TestPairMissingNeedsMoreThanOneFile(t *testing.T) {
	dir := t.TempDir()
	rep := run(t, Options{}, write(t, dir, "solo_R1.fastq", fastqWithHeaders(numbered(10, "")...)))
	if _, flagged := missingMateOf(rep.Files[0]); flagged {
		t.Fatal("a single-file run must not claim a missing mate")
	}
}

func TestPairMissingIsNotRaisedForCompletePairsOrDuplicatedMates(t *testing.T) {
	dir := t.TempDir()
	reads := fastqWithHeaders(numbered(10, "")...)
	complete := run(t, Options{}, write(t, dir, "ok_R1.fastq", reads), write(t, dir, "ok_R2.fastq", reads))
	dup := run(t, Options{},
		write(t, dir, "dup_R1.fastq", reads),
		write(t, dir, "dup_R1.fastq.gz", gzipped(t, reads)),
		write(t, dir, "dup_R2.fastq", reads))
	for _, rep := range []Report{complete, dup} {
		for _, f := range rep.Files {
			if _, flagged := missingMateOf(f); flagged {
				t.Errorf("%s flagged", f.Name)
			}
		}
	}
}

func TestMatesInDifferentDirectoriesOrWithDifferentChunksEachMissTheirMate(t *testing.T) {
	dir := t.TempDir()
	reads := fastqWithHeaders(numbered(10, "")...)
	apart := run(t, Options{}, writeIn(t, dir, "a", "s_R1.fastq", reads), writeIn(t, dir, "b", "s_R2.fastq", reads))
	chunks := run(t, Options{}, write(t, dir, "S_R1_001.fastq", reads), write(t, dir, "S_R2_002.fastq", reads))
	for name, rep := range map[string]Report{"directories": apart, "chunks": chunks} {
		for _, f := range rep.Files {
			if _, flagged := missingMateOf(f); !flagged {
				t.Errorf("%s: %s not flagged", name, f.Name)
			}
		}
	}
}

func spots(accession string, n int, mate string) []string {
	headers := make([]string, n)
	for i := range headers {
		headers[i] = fmt.Sprintf("%s.%d%s", accession, i+1, mate)
	}
	return headers
}

func TestFastqDumpReadIDsPairWithoutPairIDs(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "SRR001666_1.fastq", fastqWithHeaders(spots("SRR001666", 10, ".1")...))
	r2 := write(t, dir, "SRR001666_2.fastq", fastqWithHeaders(spots("SRR001666", 10, ".2")...))
	rep := run(t, Options{}, r1, r2)
	for _, f := range rep.Files {
		if got := failed(f); len(got) != 0 {
			t.Errorf("%s: failed %v", f.Name, got)
		}
	}
}

func TestDistinctSpotsEndingInDotOneAndDotTwoStillGivePairIDs(t *testing.T) {
	dir := t.TempDir()
	r1 := write(t, dir, "SRR1_1.fastq", fastqWithHeaders("SRR1.1", "SRR1.3"))
	r2 := write(t, dir, "SRR1_2.fastq", fastqWithHeaders("SRR1.2", "SRR1.3"))
	rep := run(t, Options{}, r1, r2)
	if got := failed(rep.Files[0]); fmt.Sprint(got) != fmt.Sprint([]string{diagnoses.PairIDs}) {
		t.Errorf("failed %v, want [pair_ids]", got)
	}
}
