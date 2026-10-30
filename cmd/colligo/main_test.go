package main

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/internal/exttool"
	"github.com/colligo-go/colligo/verifier"
)

func fastq(prefix string, reads int) []byte {
	var b bytes.Buffer
	for i := 0; i < reads; i++ {
		fmt.Fprintf(&b, "@%s.%d/1\nACGTACGTAC\n+\nFFFFFFFFFF\n", prefix, i+1)
	}
	return b.Bytes()
}

func gz(data []byte) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write(data)
	w.Close()
	return b.Bytes()
}

func put(t *testing.T, dir, base string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, base)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func invoke(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestUsageErrorsExit1(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"nope"},
		{"check"},
		{"check", "--format", "bogus", "x"},
		{"check", "--expected", "md5", "x"},
		{"check", "--expected", "md5=" + strings.Repeat("0", 32), "a", "b"},
		{"check", "--no-such-flag", "x"},
	} {
		if code, _, _ := invoke(args...); code != verifier.ExitUsage {
			t.Errorf("%v: exit %d, want 1", args, code)
		}
	}
}

func TestCheckCleanPairWithFlagsAfterPaths(t *testing.T) {
	dir := t.TempDir()
	r1 := put(t, dir, "SRR2133898_1.fastq.gz", gz(fastq("SRR", 100)))
	r2 := put(t, dir, "SRR2133898_2.fastq.gz", gz(fastq("SRR", 100)))
	code, out, errText := invoke("check", r1, r2, "--json")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errText)
	}
	var decoded struct {
		ExitCode int `json:"exit_code"`
		Files    []struct {
			Status string `json:"status"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if len(decoded.Files) != 2 || decoded.Files[0].Status != "verified" {
		t.Fatalf("%+v", decoded)
	}
}

func TestCheckDirectoryWithPairAndTruncatedFileNeedsNoConfiguration(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "a_R1.fastq.gz", gz(fastq("A", 200)))
	put(t, dir, "a_R2.fastq.gz", gz(fastq("A", 200)))
	cut := gz(fastq("B", 2000))
	put(t, dir, "b.fastq.gz", cut[:len(cut)/3])
	put(t, dir, "README.txt", []byte("notes"))
	code, out, _ := invoke("check", dir)
	if code != verifier.ExitIncomplete {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, want := range []string{"The gzip structure is broken", "probable cause:", "suggested action:", "skipped: not a recognised data file"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if code, _, _ := invoke("check", dir, "--permissive"); code != 0 {
		t.Fatalf("permissive exit %d", code)
	}
}

func TestReadMissingInR2IsExit3(t *testing.T) {
	dir := t.TempDir()
	r1 := put(t, dir, "s_R1.fastq", fastq("S", 50))
	r2 := put(t, dir, "s_R2.fastq", fastq("S", 49))
	code, out, _ := invoke("check", r1, r2)
	if code != verifier.ExitIncomplete || !strings.Contains(out, "different number of reads") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestHTMLPageRenamedIsExit2(t *testing.T) {
	p := put(t, t.TempDir(), "annotation.gtf.gz", []byte("<!DOCTYPE html><title>Error</title>"))
	code, out, _ := invoke("check", p)
	if code != verifier.ExitFatal || !strings.Contains(out, "html_body") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestExpectedChecksum(t *testing.T) {
	data := fastq("S", 20)
	p := put(t, t.TempDir(), "s.fastq", data)
	sum := md5.Sum(data)
	if code, out, _ := invoke("check", "--expected", "md5="+hex.EncodeToString(sum[:]), p); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if code, _, _ := invoke("check", "--expected", "md5="+strings.Repeat("a", 32), p); code != verifier.ExitFatal {
		t.Fatalf("exit %d", code)
	}
}

func TestValidateOnlyChecksStructure(t *testing.T) {
	dir := t.TempDir()
	good := put(t, dir, "g.fastq", fastq("S", 20))
	bad := put(t, dir, "b.fastq", fastq("S", 20)[:30])
	code, out, _ := invoke("validate", good, bad)
	if code != verifier.ExitIncomplete || !strings.Contains(out, "valid") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if code, _, _ := invoke("validate", good); code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestQuickIsNeverReportedAsVerified(t *testing.T) {
	p := put(t, t.TempDir(), "s.fastq", fastq("S", 20))
	code, out, _ := invoke("check", "--quick", p)
	if code != 0 || strings.Contains(out, "\n  verified:") || !strings.Contains(out, "quick: fastq") || !strings.Contains(out, "content was not read in full") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestCrossCheckFilesAreCompatibleWithSha256sum(t *testing.T) {
	dir := t.TempDir()
	p := put(t, dir, "s.fastq", fastq("S", 30))
	t.Chdir(dir)
	if code, out, _ := invoke("check", p, "--cross-check"); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, f := range []string{"colligo.sha256", "colligo.b3"} {
		raw, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil || !strings.HasSuffix(strings.TrimSpace(string(raw)), p) {
			t.Fatalf("%s: %v %q", f, err, raw)
		}
	}
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Fatal("sha256sum not found: the cross-check cannot be confirmed")
	}
	if out, err := exec.Command("sha256sum", "-c", "colligo.sha256").CombinedOutput(); err != nil {
		t.Fatalf("sha256sum -c: %v\n%s", err, out)
	}
	if b3sum, ok := exttool.B3Sum(t); ok {
		if out, err := exec.Command(b3sum, "-c", "colligo.b3").CombinedOutput(); err != nil {
			t.Fatalf("b3sum -c: %v\n%s", err, out)
		}
	}
}

func TestCertificateIsWrittenNextToTheFile(t *testing.T) {
	data := gz(fastq("S", 40))
	p := put(t, t.TempDir(), "s.fastq.gz", data)
	if code, out, _ := invoke("check", "--certificate", p); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	raw, err := os.ReadFile(p + ".colligo-cert.json")
	if err != nil {
		t.Fatal(err)
	}
	var cert struct {
		SchemaVersion int    `json:"schema_version"`
		Size          int64  `json:"size"`
		BLAKE3Root    string `json:"blake3_root"`
		GroupSize     int64  `json:"group_size"`
		Facts         []struct {
			Kind             string `json:"kind"`
			ExtractorVersion string `json:"extractor_version"`
		} `json:"facts"`
	}
	if err := json.Unmarshal(raw, &cert); err != nil {
		t.Fatal(err)
	}
	if cert.SchemaVersion != 1 || cert.Size != int64(len(data)) || len(cert.BLAKE3Root) != 64 || cert.GroupSize != 8<<20 || len(cert.Facts) == 0 {
		t.Fatalf("%+v", cert)
	}
	if bytes.Contains(raw, []byte(p)) {
		t.Fatal("the certificate must not carry the file path")
	}
}

func TestExplicitFileIsCheckedEvenIfUnrecognised(t *testing.T) {
	p := put(t, t.TempDir(), "mystery.dat", []byte{0, 1, 2, 3})
	code, out, _ := invoke("check", p)
	if code != verifier.ExitIncomplete || !strings.Contains(out, "format_unknown") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestVersion(t *testing.T) {
	if code, out, _ := invoke("version"); code != 0 || !strings.HasPrefix(out, "colligo ") {
		t.Fatalf("%d %q", code, out)
	}
}

func TestMissingPathIsAUsageErrorThatNamesThePath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "typo.fastq.gz")
	code, out, errText := invoke("check", missing)
	if code != verifier.ExitUsage || out != "" || !strings.Contains(errText, missing) {
		t.Fatalf("exit %d out %q err %q", code, out, errText)
	}
}

func TestFatalInOneFileStillChecksTheOthers(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "annotation.gtf.gz", []byte("<!DOCTYPE html><title>Error</title>"))
	put(t, dir, "reads.fastq", fastq("S", 10))
	code, out, _ := invoke("check", dir)
	if code != verifier.ExitFatal {
		t.Fatalf("exit %d, want 2\n%s", code, out)
	}
	if !strings.Contains(out, "html_body") || !strings.Contains(out, "verified: fastq") {
		t.Fatalf("the healthy file must still be checked and reported:\n%s", out)
	}
}
