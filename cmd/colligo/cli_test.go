package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/colligo-go/colligo/verifier"
)

var binaryPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "colligo-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binaryPath = filepath.Join(dir, "colligo")
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestInterruptDuringALargeFileExits4(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.fastq")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriterSize(f, 1<<20)
	chunk := fastq("BIG", 20000)
	for written := 0; written < 200<<20; written += len(chunk) {
		w.Write(chunk)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cmd := exec.Command(binaryPath, "check", p)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		counters, _ := os.ReadFile(fmt.Sprintf("/proc/%d/io", cmd.Process.Pid))
		var rchar int64
		for _, line := range strings.Split(string(counters), "\n") {
			fmt.Sscanf(line, "rchar: %d", &rchar)
		}
		if rchar > 8<<20 {
			break
		}
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			t.Fatal("the process never started reading")
		}
		time.Sleep(time.Millisecond)
	}
	cmd.Process.Signal(syscall.SIGINT)
	err = cmd.Wait()
	var exit *exec.ExitError
	if err == nil || !asExit(err, &exit) || exit.ExitCode() != verifier.ExitInterrupted {
		t.Fatalf("want exit 4, got %v\nstdout %s\nstderr %s", err, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "interrupted") {
		t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
	}
}

func asExit(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	*target = e
	return ok
}

func TestDoubleDashEndsTheFlagsEvenForNamesStartingWithDash(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "-a.fastq", fastq("A", 5))
	put(t, dir, "-b.fastq", fastq("B", 5))
	t.Chdir(dir)
	code, out, errText := invoke("check", "--", "-a.fastq", "-b.fastq")
	if code != 0 || strings.Count(out, "verified: fastq") != 2 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errText)
	}
	code, out, _ = invoke("check", "--json", "--", "-a.fastq")
	if code != 0 || !json.Valid([]byte(out)) {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if code, _, _ := invoke("check", "-a.fastq"); code != verifier.ExitUsage {
		t.Fatalf("a dash name without -- must be a flag error, exit %d", code)
	}
}

func TestExpectedHashIsCaseInsensitive(t *testing.T) {
	data := fastq("S", 20)
	p := put(t, t.TempDir(), "s.fastq", data)
	sum := md5.Sum(data)
	upper := strings.ToUpper(hex.EncodeToString(sum[:]))
	if code, out, _ := invoke("check", "--expected", "md5="+upper, p); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestSameFileTwiceIsReportedTwiceWithoutPairClaims(t *testing.T) {
	p := put(t, t.TempDir(), "s_R1.fastq", fastq("S", 10))
	code, out, _ := invoke("check", p, p)
	if code != 0 || strings.Count(out, "verified: fastq") != 2 || strings.Contains(out, "pair_") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestClosedStdoutDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3000; i++ {
		put(t, dir, fmt.Sprintf("f%04d.fastq", i), fastq("S", 1))
	}
	cmd := exec.Command(binaryPath, "check", dir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	io.ReadFull(pipe, buf)
	pipe.(io.Closer).Close()
	cmd.Wait()
	if strings.Contains(stderr.String(), "panic") || strings.Contains(stderr.String(), "goroutine ") {
		t.Fatalf("panic on a closed stdout:\n%s", stderr.String())
	}
}

func TestJSONWithManyFilesIsOneValidDocument(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 300; i++ {
		put(t, dir, fmt.Sprintf("f%03d.fastq", i), fastq("S", 3))
	}
	cmd := exec.Command(binaryPath, "check", "--json", dir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	dec := json.NewDecoder(&stdout)
	var doc struct {
		Files []json.RawMessage `json:"files"`
	}
	if err := dec.Decode(&doc); err != nil || len(doc.Files) != 300 {
		t.Fatalf("%v, %d files", err, len(doc.Files))
	}
	if dec.More() {
		t.Fatal("stdout carries more than the JSON document")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr: %s", stderr.String())
	}
}

func TestOddFileNamesInReportJSONAndChecksums(t *testing.T) {
	dir := t.TempDir()
	names := []string{"with space.fastq", "açaí.fastq", `back\slash.fastq`, "new\nline.fastq", `both\` + "\n" + `x.fastq`}
	for _, n := range names {
		put(t, dir, n, fastq("S", 5))
	}
	t.Chdir(dir)
	args := append([]string{"check", "--cross-check", "--json"}, names...)
	code, out, errText := invoke(args...)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errText)
	}
	var doc struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Files) != len(names) {
		t.Fatalf("%v\n%s", err, out)
	}
	for i, f := range doc.Files {
		if f.Path != names[i] {
			t.Errorf("JSON path %q, want %q", f.Path, names[i])
		}
	}
	_, text, _ := invoke(append([]string{"check"}, names...)...)
	for _, n := range []string{"with space.fastq", "açaí.fastq"} {
		if !strings.Contains(text, n) {
			t.Errorf("report lacks %q", n)
		}
	}
	raw, err := os.ReadFile("colligo.sha256")
	if err != nil {
		t.Fatal(err)
	}
	escaped := 0
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if strings.HasPrefix(line, `\`) {
			escaped++
		}
	}
	if escaped != 3 {
		t.Fatalf("%d escaped lines, want 3:\n%s", escaped, raw)
	}
	if outp, err := exec.Command("sha256sum", "-c", "colligo.sha256").CombinedOutput(); err != nil {
		t.Fatalf("sha256sum -c: %v\n%s", err, outp)
	}
}

func TestSymbolicLinks(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "real.fastq", fastq("S", 5))
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	put(t, sub, "inner.fastq", fastq("I", 5))
	os.Symlink(sub, filepath.Join(dir, "linkdir"))
	os.Symlink(dir, filepath.Join(sub, "loop"))
	os.Symlink(filepath.Join(dir, "missing.fastq"), filepath.Join(dir, "broken.fastq"))

	code, out, _ := invoke("check", "--recursive", dir)
	if code != verifier.ExitFatal {
		t.Fatalf("a broken link is unreadable, want exit 2, got %d\n%s", code, out)
	}
	if strings.Count(out, "symbolic link to a directory, not followed") != 2 {
		t.Fatalf("directory links must be labelled as such:\n%s", out)
	}
	if strings.Contains(out, "not a recognised data file") {
		t.Fatalf("a directory link was labelled as unrecognised data:\n%s", out)
	}
	if strings.Count(out, "verified: fastq") != 2 {
		t.Fatalf("each real file once:\n%s", out)
	}
	if !strings.Contains(out, "unreadable") {
		t.Fatalf("the broken link must be reported:\n%s", out)
	}
	_, js, _ := invoke("check", "--recursive", "--json", dir)
	if !strings.Contains(js, "skipped_directory_links") {
		t.Fatalf("JSON lacks the directory links:\n%s", js)
	}
}

func TestHugeJobsValueAndDeepRecursion(t *testing.T) {
	root := t.TempDir()
	put(t, root, "top.fastq", fastq("T", 3))
	p := root
	for i := 0; i < 50; i++ {
		p = filepath.Join(p, "d")
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	put(t, p, "deep.fastq", fastq("D", 3))
	code, out, errText := invoke("check", "--recursive", "--jobs", "1000000000", root)
	if code != 0 || strings.Count(out, "verified: fastq") != 2 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errText)
	}
}
