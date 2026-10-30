package facts

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/gzip"
)

func perfDir(tb testing.TB) string {
	tb.Helper()
	if dir := os.Getenv("COLLIGO_PERF_DIR"); dir != "" {
		return dir
	}
	return tb.TempDir()
}

func writePerfFastq(w io.Writer, megabytes int) {
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
}

func perfFastqGz(tb testing.TB, megabytes int) (path string, size int64) {
	tb.Helper()
	path = filepath.Join(perfDir(tb), fmt.Sprintf("perf_%dmb.fastq.gz", megabytes))
	if st, err := os.Stat(path); err == nil {
		return path, st.Size()
	}
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	w, _ := gzip.NewWriterLevel(f, 6)
	writePerfFastq(w, megabytes)
	w.Close()
	f.Close()
	st, _ := os.Stat(path)
	return path, st.Size()
}

func perfFastq(tb testing.TB, megabytes int) (path string, size int64) {
	tb.Helper()
	path = filepath.Join(perfDir(tb), fmt.Sprintf("perf_%dmb.fastq", megabytes))
	if st, err := os.Stat(path); err == nil {
		return path, st.Size()
	}
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	w := bufio.NewWriterSize(f, 1<<20)
	writePerfFastq(w, megabytes)
	w.Flush()
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
