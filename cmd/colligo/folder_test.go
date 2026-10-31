package main

import (
	"strings"
	"testing"

	"github.com/colligo-go/colligo/verifier"
)

func TestAFolderWithNothingToCheckIsBlocking(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "README.txt", []byte("notes about the run\n"))
	code, out, _ := invoke("check", dir)
	if code != verifier.ExitIncomplete || !strings.Contains(out, "NOTHING CHECKED") || !strings.Contains(out, "exit 3") {
		t.Fatalf("exit %d, want 3\n%s", code, out)
	}
	if code, out, _ := invoke("check", "--json", dir); code != verifier.ExitIncomplete || !strings.Contains(out, `"exit_code": 3`) {
		t.Fatalf("--json: exit %d\n%s", code, out)
	}
	if code, _, _ := invoke("check", "--permissive", dir); code != verifier.ExitOK {
		t.Fatalf("--permissive: exit %d, want 0", code)
	}
}

func TestAnEmptyFolderIsBlocking(t *testing.T) {
	if code, out, _ := invoke("check", t.TempDir()); code != verifier.ExitIncomplete {
		t.Fatalf("exit %d, want 3\n%s", code, out)
	}
}

func TestAnSRAArchiveInAFolderIsNamedAndBlocking(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "SRR123", append([]byte("NCBI.sra"), make([]byte, 64)...))
	code, out, _ := invoke("check", dir)
	if code != verifier.ExitIncomplete || !strings.Contains(out, "input_sra") || strings.Contains(out, "NOTHING CHECKED") {
		t.Fatalf("exit %d, want 3 with input_sra\n%s", code, out)
	}
}

func TestACRAMFileInAFolderIsNamedLikeABAM(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "aligned.bin", append([]byte("CRAM\x03\x01"), make([]byte, 64)...))
	code, out, _ := invoke("check", dir)
	if code != verifier.ExitIncomplete || !strings.Contains(out, "input_alignment") {
		t.Fatalf("exit %d, want 3 with input_alignment\n%s", code, out)
	}
}

func TestAFastQCZipNextToTheReadsDoesNotFailTheFolder(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "sample.fastq", fastq("S", 10))
	put(t, dir, "sample_fastqc.zip", append([]byte("PK\x03\x04"), make([]byte, 64)...))
	code, out, _ := invoke("check", dir)
	if code != verifier.ExitOK || strings.Contains(out, "input_archive") {
		t.Fatalf("exit %d, want 0: a FastQC report beside the reads is not a mistake\n%s", code, out)
	}
}

func TestIdentifyWithNothingToIdentifyIsBlocking(t *testing.T) {
	if code, out, _ := invoke("identify", t.TempDir()); code != verifier.ExitIncomplete || !strings.Contains(out, "NOTHING IDENTIFIED") {
		t.Fatalf("exit %d, want 3\n%s", code, out)
	}
}
