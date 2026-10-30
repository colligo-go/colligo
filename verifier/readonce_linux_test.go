package verifier

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/secret"
)

func processReadCounter(t *testing.T) (value, selfRead int64) {
	t.Helper()
	text, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Fatalf("per-process read accounting is unavailable: %v", err)
	}
	for _, line := range strings.Split(string(text), "\n") {
		if field, ok := strings.CutPrefix(line, "rchar: "); ok {
			n, err := strconv.ParseInt(field, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return n, int64(len(text))
		}
	}
	t.Fatal("/proc/self/io has no rchar line")
	return 0, 0
}

func bytesRead(t *testing.T, fn func()) int64 {
	t.Helper()
	before, selfRead := processReadCounter(t)
	fn()
	after, _ := processReadCounter(t)
	return after - before - selfRead
}

const schedulerWakeups = 64

func leastBytesRead(t *testing.T, want int64, fn func()) int64 {
	t.Helper()
	least := bytesRead(t, fn)
	for attempt := 1; attempt < 5 && least != want; attempt++ {
		least = min(least, bytesRead(t, fn))
	}
	return least
}

func TestCheckFileReadsEveryByteExactlyOnce(t *testing.T) {
	dir := t.TempDir()
	plain := fastqText("SRR1", 40_000, "ACGTACGTACGTACGT")
	files := map[string][]byte{
		"empty.fastq":          nil,
		"tiny.fastq":           fastqText("SRR1", 3, "ACGT"),
		"plain.fastq":          plain,
		"zipped.fastq.gz":      gzipped(t, plain),
		"large.fastq":          fastqText("SRR1", 90_000, "ACGTACGTACGTACGT"),
		"html.gtf.gz":          []byte("<!DOCTYPE html><title>Login</title>"),
		"unrecognised.dat":     {0, 1, 2, 3, 4, 5, 6, 7},
		"truncated.fastq.gz":   gzipped(t, plain)[:5000],
		"not-compressed.fq.gz": plain[:4000],
	}
	for _, structure := range []bool{false, true} {
		for base, data := range files {
			name := write(t, dir, base, data)
			got := leastBytesRead(t, int64(len(data)), func() {
				if _, err := CheckFile(context.Background(), name, Options{Structure: structure}); err != nil {
					t.Fatal(err)
				}
			})
			if got != int64(len(data)) {
				t.Errorf("%s (structure only %v): %d bytes read from a file of %d", base, structure, got, len(data))
			}
		}
	}
}

func TestCheckAllReadsEveryByteOfEveryFileOnce(t *testing.T) {
	dir := t.TempDir()
	var names []secret.Name
	var total int64
	for i, records := range []int{10, 5_000, 90_000} {
		data := fastqText("SRR1", records, "ACGTACGT")
		names = append(names, write(t, dir, "sample"+strconv.Itoa(i)+"_R1.fastq", data))
		names = append(names, write(t, dir, "sample"+strconv.Itoa(i)+"_R2.fastq", data))
		total += 2 * int64(len(data))
	}
	got := leastBytesRead(t, total, func() { run(t, Options{Jobs: 4}, names...) })
	if got < total || got > total+schedulerWakeups {
		t.Fatalf("%d bytes read for %d bytes of files, pair evaluation included", got, total)
	}
}

func TestDiscoverReadsAtMostTheClassificationSamplePerFile(t *testing.T) {
	dir := t.TempDir()
	var sample int64
	for base, data := range map[string][]byte{
		"big.fastq":   fastqText("SRR1", 100_000, "ACGTACGT"),
		"small.fastq": fastqText("SRR1", 3, "ACGT"),
		"notes.txt":   []byte("hello"),
		"page.gtf.gz": []byte("<!DOCTYPE html>"),
	} {
		write(t, dir, base, data)
		sample += min(int64(len(data)), quickSample)
	}
	var names []secret.Name
	got := bytesRead(t, func() {
		var err error
		names, _, _, err = Discover(context.Background(), secret.NewName(dir), false)
		if err != nil {
			t.Fatal(err)
		}
	})
	if len(names) == 0 || got == 0 || got > sample {
		t.Fatalf("discovery read %d bytes, expected between 1 and %d (%d candidates)", got, sample, len(names))
	}
}
