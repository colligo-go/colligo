package facts

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func perfFastqGz(tb testing.TB, megabytes int) (path string, size int64) {
	tb.Helper()
	dir := os.Getenv("COLLIGO_PERF_DIR")
	if dir == "" {
		dir = tb.TempDir()
	}
	path = filepath.Join(dir, fmt.Sprintf("perf_%dmb.fastq.gz", megabytes))
	if st, err := os.Stat(path); err == nil {
		return path, st.Size()
	}
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	w, _ := gzip.NewWriterLevel(f, 6)
	rng := rand.New(rand.NewSource(11))
	var rec bytes.Buffer
	quals := []byte("5?BCDEFGHIJ")
	written := 0
	for i := 0; written < megabytes<<20; i++ {
		rec.Reset()
		seq := make([]byte, 100)
		qual := make([]byte, 100)
		for j := range seq {
			seq[j] = "ACGT"[rng.Intn(4)]
			qual[j] = quals[min(rng.Intn(len(quals)), rng.Intn(len(quals)))]
		}
		fmt.Fprintf(&rec, "@SRR1.%d/1\n%s\n+\n%s\n", i+1, seq, qual)
		w.Write(rec.Bytes())
		written += rec.Len()
	}
	w.Close()
	f.Close()
	st, _ := os.Stat(path)
	return path, st.Size()
}

func timeIt(fn func()) time.Duration {
	start := time.Now()
	fn()
	return time.Since(start)
}

func BenchmarkComputeFastqGz(b *testing.B) {
	path, size := perfFastqGz(b, 256)
	b.SetBytes(size)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, _ := os.Open(path)
		if _, err := Compute(context.Background(), f, Options{Hashes: true}); err != nil {
			b.Fatal(err)
		}
		f.Close()
	}
}

func TestPerformanceFloor(t *testing.T) {
	if os.Getenv("COLLIGO_PERF") == "" {
		t.Skip("set COLLIGO_PERF=1 to run the performance benchmark")
	}
	if _, err := exec.LookPath("zcat"); err != nil {
		t.Fatal("zcat not found: the floor cannot be measured")
	}
	path, size := perfFastqGz(t, 1024)
	best := func(fn func()) time.Duration {
		d := timeIt(fn)
		for i := 0; i < 2; i++ {
			d = min(d, timeIt(fn))
		}
		return d
	}
	floor := best(func() {
		if err := exec.Command("sh", "-c", fmt.Sprintf("zcat %q | wc -l >/dev/null", path)).Run(); err != nil {
			t.Fatal(err)
		}
	})
	ours := best(func() {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := Compute(context.Background(), f, Options{Hashes: true}); err != nil {
			t.Fatal(err)
		}
	})
	ratio := float64(ours) / float64(floor)
	t.Logf("file %.0f MiB compressed: zcat|wc -l = %v, check = %v, ratio = %.1f%% (target <= 110%%)",
		float64(size)/(1<<20), floor, ours, 100*ratio)
	if ratio > 1.10 {
		t.Fatalf("check is %.1f%% of the floor, target is 110%%", 100*ratio)
	}
}
