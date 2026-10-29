package manifest

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/internal/exttool"
	"github.com/colligo-go/colligo/verifier"
)

func exitCode(t *testing.T, exe, cert string, extra ...string) (int, string) {
	t.Helper()
	args := append([]string{mustAbs(t, "../tools/verify_cert.py"), cert}, extra...)
	out, err := exec.Command(exe, args...).CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatal(err)
	}
	return exit.ExitCode(), string(out)
}

func cutTail(data []byte, n int) []byte { return data[:len(data)-n] }

func assertionOf(t *testing.T, doc map[string]any, id string) map[string]any {
	t.Helper()
	for _, a := range doc["assertions"].([]any) {
		assertion := a.(map[string]any)
		if assertion["id"] == id {
			return assertion
		}
	}
	t.Fatalf("no assertion %s in the certificate", id)
	return nil
}

func TestIndependentVerifierChecksTheAssertionsOfAnInvalidFile(t *testing.T) {
	exe := exttool.Python(t)
	dir := t.TempDir()
	good := gz(fastq("SRR1", 800, 5))
	wrongSize := int64(1)
	wrongReads := int64(5)

	cases := []struct {
		name string
		base string
		data []byte
		opts verifier.Options
		id   string
		edit func(map[string]any)
	}{
		{"truncated gzip", "cut.fastq.gz", good[:len(good)/2], verifier.Options{}, "structure_invalid",
			func(a map[string]any) { a["passed"] = true; a["observed"] = "valid" }},
		{"truncated gzip with only passed edited", "cut2.fastq.gz", good[:len(good)/2], verifier.Options{}, "structure_invalid",
			func(a map[string]any) { a["passed"] = true }},
		{"truncated fastq", "cut.fastq", cutTail(fastq("S", 50, 1), 5), verifier.Options{}, "structure_invalid",
			func(a map[string]any) { a["passed"] = true; a["observed"] = "valid" }},
		{"count mismatch", "count.fastq", fastq("S", 50, 2), verifier.Options{Expected: verifier.Expected{Reads: &wrongReads}}, "count_mismatch",
			func(a map[string]any) { a["passed"] = true }},
		{"count observed", "count2.fastq", fastq("S", 50, 2), verifier.Options{Expected: verifier.Expected{Reads: &wrongReads}}, "count_mismatch",
			func(a map[string]any) { a["observed"] = "5"; a["passed"] = true }},
		{"checksum mismatch", "sum.fastq", fastq("S", 50, 3), verifier.Options{Expected: verifier.Expected{MD5: strings.Repeat("0", 32)}}, "checksum_mismatch",
			func(a map[string]any) { a["passed"] = true }},
		{"size mismatch", "size.fastq", fastq("S", 50, 4), verifier.Options{Expected: verifier.Expected{Size: &wrongSize}}, "size_mismatch",
			func(a map[string]any) { a["passed"] = true }},
		{"size observed", "size2.fastq", fastq("S", 50, 4), verifier.Options{Expected: verifier.Expected{Size: &wrongSize}}, "size_mismatch",
			func(a map[string]any) { a["observed"] = "1"; a["passed"] = true }},
	}
	for _, c := range cases {
		cert := certifyWith(t, dir, c.base, c.data, c.opts)
		data := strings.TrimSuffix(cert, CertificateSuffix)
		if code, out := exitCode(t, exe, cert, data); code != 0 {
			t.Fatalf("%s: the honest certificate of an invalid file must agree, exit %d\n%s", c.name, code, out)
		}
		tampered := mutate(t, cert, func(d map[string]any) { c.edit(assertionOf(t, d, c.id)) })
		if code, out := exitCode(t, exe, tampered, data); code != 1 {
			t.Errorf("%s: tampered %s exit %d, want 1\n%s", c.name, c.id, code, out)
		}
	}
}

func TestIndependentVerifierRejectsAValidStructureClaimedInvalid(t *testing.T) {
	exe := exttool.Python(t)
	cert := certify(t, t.TempDir(), "ok.fastq", fastq("S", 50, 1))
	data := strings.TrimSuffix(cert, CertificateSuffix)
	tampered := mutate(t, cert, func(d map[string]any) {
		a := assertionOf(t, d, "structure_invalid")
		a["passed"] = false
		a["observed"] = "line count is not a multiple of 4 (at byte 10)"
	})
	if code, out := exitCode(t, exe, tampered, data); code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, out)
	}
}

func TestIndependentVerifierCannotJudgeAMalformedCertificate(t *testing.T) {
	exe := exttool.Python(t)
	cert := certify(t, t.TempDir(), "s.fastq", fastq("S", 20, 1))
	data := strings.TrimSuffix(cert, CertificateSuffix)

	edits := map[string]func(map[string]any){
		"missing size":             func(d map[string]any) { delete(d, "size") },
		"missing blake3 root":      func(d map[string]any) { delete(d, "blake3_root") },
		"missing group size":       func(d map[string]any) { delete(d, "group_size") },
		"missing assertions":       func(d map[string]any) { delete(d, "assertions") },
		"missing facts":            func(d map[string]any) { delete(d, "facts") },
		"missing tool":             func(d map[string]any) { delete(d, "tool") },
		"missing fact kind":        func(d map[string]any) { delete(d["facts"].([]any)[0].(map[string]any), "kind") },
		"missing assertion passed": func(d map[string]any) { delete(assertionOf(t, d, "html_body"), "passed") },
		"missing assertion id":     func(d map[string]any) { delete(d["assertions"].([]any)[0].(map[string]any), "id") },
		"wrong type for size":      func(d map[string]any) { d["size"] = "17" },
		"wrong type for passed":    func(d map[string]any) { assertionOf(t, d, "html_body")["passed"] = "yes" },
		"unknown top level field":  func(d map[string]any) { d["note"] = "extra" },
		"unknown assertion field":  func(d map[string]any) { assertionOf(t, d, "html_body")["note"] = "extra" },
		"unknown fact field":       func(d map[string]any) { d["facts"].([]any)[0].(map[string]any)["note"] = "extra" },
		"unknown tool field":       func(d map[string]any) { d["tool"].(map[string]any)["note"] = "extra" },
	}
	for name, edit := range edits {
		tampered := mutate(t, cert, edit)
		if code, out := exitCode(t, exe, tampered, data); code != 2 {
			t.Errorf("%s: exit %d, want 2\n%s", name, code, out)
		}
	}
}

func TestIndependentVerifierRejectsAValidCertificateAgainstAnotherFile(t *testing.T) {
	exe := exttool.Python(t)
	dir := t.TempDir()
	cert := certify(t, dir, "a.fastq", fastq("S", 100, 1))
	same := filepath.Join(dir, "same-size.fastq")
	raw, err := os.ReadFile(strings.TrimSuffix(cert, CertificateSuffix))
	if err != nil {
		t.Fatal(err)
	}
	flipped := bytes.Clone(raw)
	flipped[len(flipped)/2] ^= 0x01
	if err := os.WriteFile(same, flipped, 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other.fastq")
	if err := os.WriteFile(other, fastq("O", 7, 2), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{same, other} {
		if code, out := exitCode(t, exe, cert, path); code != 1 {
			t.Errorf("%s: exit %d, want 1\n%s", filepath.Base(path), code, out)
		}
	}
}

func TestIndependentVerifierChecksTheRootOfAMultiGroupFile(t *testing.T) {
	exe := exttool.Python(t)
	dir := t.TempDir()
	big := fastq("BIG", 40000, 1)
	for len(big) <= 9<<20 {
		big = append(big, fastq("BIG", 40000, int64(len(big)))...)
	}
	cert := certify(t, dir, "big.fastq", big)
	data := strings.TrimSuffix(cert, CertificateSuffix)
	if code, out := exitCode(t, exe, cert, data); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	wrongRoot := mutate(t, cert, func(d map[string]any) { d["blake3_root"] = strings.Repeat("0", 64) })
	if code, out := exitCode(t, exe, wrongRoot, data); code != 1 {
		t.Fatalf("a wrong root exit %d, want 1\n%s", code, out)
	}
}

func TestIndependentVerifierListsWhatItDidNotRecompute(t *testing.T) {
	exe := exttool.Python(t)
	dir := t.TempDir()
	cert := certify(t, dir, "genome.fa", []byte(">chr1\nACGTACGT\n>chr2\nGGCC\n"))
	code, out := exitCode(t, exe, cert)
	if code != 0 || !strings.HasPrefix(out, "AGREE\n") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, want := range []string{"UNCHECKED fact ", "UNCHECKED assertion html_body"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	fq := certify(t, dir, "r.fastq", fastq("S", 20, 1))
	_, out = exitCode(t, exe, fq)
	if strings.Contains(out, "UNCHECKED fact records") || strings.Contains(out, "UNCHECKED fact digests") {
		t.Errorf("recomputed facts listed as unchecked:\n%s", out)
	}
}
