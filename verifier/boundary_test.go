package verifier

import (
	"bytes"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
)

func firstRecords(data []byte, n int) []byte {
	end := 0
	for lines := 0; lines < 4*n; lines++ {
		end += bytes.IndexByte(data[end:], '\n') + 1
	}
	return bytes.Clone(data[:end])
}

func TestFastqCutOnARecordBoundaryPassesStructureAndIsCaughtByDeclaredReads(t *testing.T) {
	dir := t.TempDir()
	full := fastqText("SRR1", 50, "ACGTACGT")
	cut := write(t, dir, "cut.fastq", firstRecords(full, 47))

	rep := run(t, Options{Structure: true}, cut)
	if f := rep.Files[0]; f.Status != StatusValid || len(failed(f)) != 0 {
		t.Fatalf("structure alone must accept a cut on a record boundary: %s %v", f.Status, failed(f))
	}

	reads := int64(50)
	rep = run(t, Options{Expected: Expected{Reads: &reads}}, cut)
	f := rep.Files[0]
	if got := failed(f); len(got) != 1 || got[0] != diagnoses.CountMismatch {
		t.Fatalf("declared reads must catch it: %v", got)
	}
	if rep.ExitCode(false) != ExitIncomplete {
		t.Fatalf("exit code %d", rep.ExitCode(false))
	}
}

func TestGzipCutOnAMemberBoundaryPassesStructureAndIsCaughtByDeclaredReads(t *testing.T) {
	dir := t.TempDir()
	first, second := fastqText("SRR1", 30, "ACGTACGT"), fastqText("SRR1b", 20, "ACGTACGT")
	cut := write(t, dir, "cut.fastq.gz", gzipped(t, first))
	whole := write(t, dir, "whole.fastq.gz", append(gzipped(t, first), gzipped(t, second)...))

	rep := run(t, Options{Structure: true}, cut, whole)
	for _, f := range rep.Files {
		if f.Status != StatusValid {
			t.Fatalf("%s: %s %v", f.Name, f.Status, failed(f))
		}
	}

	reads := int64(50)
	rep = run(t, Options{Expected: Expected{Reads: &reads}}, cut, whole)
	if got := failed(rep.Files[0]); len(got) != 1 || got[0] != diagnoses.CountMismatch {
		t.Fatalf("cut member: %v", got)
	}
	if got := failed(rep.Files[1]); len(got) != 0 {
		t.Fatalf("whole file: %v", got)
	}
}

func TestPairCutOnARecordBoundaryIsCaughtByThePairInvariant(t *testing.T) {
	dir := t.TempDir()
	full := fastqText("SRR1", 50, "ACGTACGT")
	r1 := write(t, dir, "s_R1.fastq", full)
	r2 := write(t, dir, "s_R2.fastq", firstRecords(full, 49))

	rep := run(t, Options{}, r1, r2)
	for _, f := range rep.Files {
		if got := failed(f); len(got) != 1 || got[0] != diagnoses.PairCount {
			t.Fatalf("%s: %v", f.Name, got)
		}
	}
}

func TestPairCutOnARecordBoundaryIsInvisibleToValidate(t *testing.T) {
	dir := t.TempDir()
	full := fastqText("SRR1", 50, "ACGTACGT")
	r1 := write(t, dir, "s_R1.fastq", full)
	r2 := write(t, dir, "s_R2.fastq", firstRecords(full, 49))

	rep := run(t, Options{Structure: true}, r1, r2)
	for _, f := range rep.Files {
		if f.Status != StatusValid || len(failed(f)) != 0 {
			t.Fatalf("%s: %s %v", f.Name, f.Status, failed(f))
		}
	}
}
