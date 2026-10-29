package manifest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/internal/exttool"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/verifier"
)

func fastq(prefix string, reads int, seed int64) []byte {
	rng := rand.New(rand.NewSource(seed))
	var b bytes.Buffer
	for i := 0; i < reads; i++ {
		seq := make([]byte, 50+rng.Intn(60))
		for j := range seq {
			seq[j] = "ACGTN"[rng.Intn(5)]
		}
		fmt.Fprintf(&b, "@%s.%d/1 len=%d\n%s\n+\n%s\n", prefix, i+1, len(seq), seq, strings.Repeat("G", len(seq)))
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

const maxDistinct = 4096

func corpus() map[string][]byte {
	good := gz(fastq("SRR1", 3000, 1))
	large := fastq("BIG", 90000, 2)
	var crlf bytes.Buffer
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&crlf, "@c%d/1\r\nACGTACGTAC\r\n+\r\nFFFFFFFFFF\r\n", i)
	}
	var lengths bytes.Buffer
	for i := 0; i < maxDistinct+200; i++ {
		fmt.Fprintf(&lengths, "@l%d\n%s\n+\n%s\n", i, strings.Repeat("A", 5+i), strings.Repeat("F", 5+i))
	}
	return map[string][]byte{
		"trailing-blank.fq": append(fastq("T", 30, 10), '\n', '\n', '\n'),
		"crlf.fastq":        crlf.Bytes(),
		"many-lengths.fq":   lengths.Bytes(),
		"plain.fastq":       fastq("P", 500, 3),
		"zipped.fastq.gz":   good,
		"truncated.fq.gz":   good[:len(good)*2/3],
		"multigroup.fq.gz":  gz(large),
		"genome.fa":         []byte(">chr1\nACGTACGT\nACGT\n>chr2\nGGCC\n"),
		"genes.gtf":         []byte("1\tsrc\tgene\t1\t10\t.\t+\t.\tgene_id \"g\";\n"),
		"empty.fastq":       nil,
		"mystery.bin":       {0, 1, 2, 3, 4, 5},
	}
}

func certify(t *testing.T, dir, base string, data []byte) string {
	t.Helper()
	return certifyWith(t, dir, base, data, verifier.Options{})
}

func certifyWith(t *testing.T, dir, base string, data []byte, opts verifier.Options) string {
	t.Helper()
	p := filepath.Join(dir, base)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := verifier.CheckFile(context.Background(), secret.NewName(p), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !Certifiable(rep) {
		t.Fatalf("%s: not certifiable (%s)", base, rep.Status)
	}
	if err := Write(rep); err != nil {
		t.Fatal(err)
	}
	return p + CertificateSuffix
}

func verify(t *testing.T, exe, cert string) (int, string) {
	t.Helper()
	root, _ := filepath.Abs("..")
	out, err := exec.Command(exe, filepath.Join(root, "tools", "verify_cert.py"), cert).CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatal(err)
	}
	return exit.ExitCode(), string(out)
}

func TestIndependentVerifierAgreesOnTheWholeCorpus(t *testing.T) {
	exe := exttool.Python(t)
	dir := t.TempDir()
	for base, data := range corpus() {
		cert := certify(t, dir, base, data)
		if code, out := verify(t, exe, cert); code != 0 {
			t.Errorf("%s: verifier exit %d\n%s", base, code, out)
		}
	}
}

func mutate(t *testing.T, cert string, edit func(map[string]any)) string {
	t.Helper()
	raw, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	edit(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.TrimSuffix(cert, CertificateSuffix) + ".tampered" + CertificateSuffix
	if err := os.WriteFile(tampered, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return tampered
}

func factValue(doc map[string]any, kind string) map[string]any {
	for _, f := range doc["facts"].([]any) {
		fact := f.(map[string]any)
		if fact["kind"] == kind {
			return fact["value"].(map[string]any)
		}
	}
	return nil
}

func TestIndependentVerifierRejectsTamperedCertificates(t *testing.T) {
	exe := exttool.Python(t)
	dir := t.TempDir()
	cert := certify(t, dir, "s.fastq.gz", gz(fastq("SRR1", 800, 5)))
	data := strings.TrimSuffix(cert, CertificateSuffix)

	edits := map[string]func(map[string]any){
		"size":       func(d map[string]any) { d["size"] = d["size"].(float64) + 1 },
		"md5":        func(d map[string]any) { d["md5"] = strings.Repeat("0", 32) },
		"sha256":     func(d map[string]any) { d["sha256"] = strings.Repeat("0", 64) },
		"blake3":     func(d map[string]any) { d["blake3_root"] = strings.Repeat("0", 64) },
		"group size": func(d map[string]any) { d["group_size"] = 1024 },
		"records":    func(d map[string]any) { factValue(d, "records")["records"] = 799 },
		"bases":      func(d map[string]any) { factValue(d, "records")["bases"] = 1 },
		"read ends":  func(d map[string]any) { factValue(d, "read_ends")["last"] = "SRR9.9" },
	}
	for name, edit := range edits {
		tampered := mutate(t, cert, edit)
		out, err := exec.Command(exe, mustAbs(t, "../tools/verify_cert.py"), tampered, data).CombinedOutput()
		if err == nil {
			t.Errorf("%s: a tampered certificate was accepted\n%s", name, out)
		}
	}
}

func TestIndependentVerifierCannotJudgeAnUnknownSchema(t *testing.T) {
	exe := exttool.Python(t)
	cert := certify(t, t.TempDir(), "s.fastq", fastq("S", 10, 9))
	tampered := mutate(t, cert, func(d map[string]any) { d["schema_version"] = 99 })
	out, err := exec.Command(exe, mustAbs(t, "../tools/verify_cert.py"), tampered, strings.TrimSuffix(cert, CertificateSuffix)).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("an unknown schema must exit 2 (cannot judge), got %v\n%s", err, out)
	}
}

func TestIndependentVerifierRejectsAChangedFile(t *testing.T) {
	exe := exttool.Python(t)
	dir := t.TempDir()
	cert := certify(t, dir, "s.fastq", fastq("S", 100, 6))
	data := strings.TrimSuffix(cert, CertificateSuffix)
	raw, _ := os.ReadFile(data)
	raw[len(raw)/2] ^= 0x01
	if err := os.WriteFile(data, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := verify(t, exe, cert); code != 1 {
		t.Fatalf("verifier exit %d, want 1", code)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestCertificateNeverCarriesThePath(t *testing.T) {
	dir := t.TempDir()
	cert := certify(t, dir, "secret-sample-name.fastq", fastq("S", 10, 7))
	raw, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("secret-sample-name")) || bytes.Contains(raw, []byte(dir)) {
		t.Fatal("the certificate carries the file name or path")
	}
}

func TestUnreadableAndQuickAreNotCertifiable(t *testing.T) {
	missing := secret.NewName(filepath.Join(t.TempDir(), "missing.fastq"))
	rep, _ := verifier.CheckFile(context.Background(), missing, verifier.Options{})
	if Certifiable(rep) {
		t.Fatal("unreadable file must not be certifiable")
	}
	p := filepath.Join(t.TempDir(), "q.fastq")
	os.WriteFile(p, fastq("S", 5, 8), 0o644)
	quick, _ := verifier.CheckFile(context.Background(), secret.NewName(p), verifier.Options{Quick: true})
	if Certifiable(quick) {
		t.Fatal("a quick result must not be certifiable")
	}
}

func TestIndependentVerifierAgreesOnTheVersionedCorpus(t *testing.T) {
	exe := exttool.Python(t)
	metas, err := filepath.Glob("../testdata/synthetic/*.meta.json")
	if err != nil || len(metas) == 0 {
		t.Fatalf("no versioned corpus: %v", err)
	}
	dir := t.TempDir()
	checked := 0
	for _, m := range metas {
		raw, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Expect struct {
				Skipped bool `json:"skipped"`
			} `json:"expect"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		if doc.Expect.Skipped {
			continue
		}
		label := strings.TrimSuffix(filepath.Base(m), ".meta.json")
		data, err := os.ReadFile(filepath.Join("../testdata/synthetic", label))
		if err != nil {
			t.Fatal(err)
		}
		cert := certify(t, dir, label, data)
		if code, out := verify(t, exe, cert); code != 0 {
			t.Errorf("%s: verifier exit %d\n%s", label, code, out)
		}
		checked++
	}
	if checked < 10 {
		t.Fatalf("only %d corpus files certified", checked)
	}
}
