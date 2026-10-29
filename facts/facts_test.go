package facts

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"testing"
	"time"

	"lukechampine.com/blake3"
)

func makeFastq(records int) []byte {
	rng := rand.New(rand.NewSource(7))
	var b bytes.Buffer
	for i := 0; i < records; i++ {
		seq := make([]byte, 100)
		for j := range seq {
			seq[j] = "ACGT"[rng.Intn(4)]
		}
		fmt.Fprintf(&b, "@SRR1.%d/1\n%s\n+\n%s\n", i+1, seq, strings.Repeat("F", 100))
	}
	return b.Bytes()
}

func gz(t testing.TB, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type countingReader struct {
	r     io.Reader
	bytes int64
	calls int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.bytes += int64(n)
	c.calls++
	return n, err
}

func factValue[T any](t *testing.T, r Result, kind string) T {
	t.Helper()
	var out T
	for _, f := range r.Facts {
		if f.Kind == kind {
			if err := json.Unmarshal(f.Value, &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	t.Fatalf("no %s fact", kind)
	return out
}

type structure struct {
	Format string `json:"format"`
	Valid  bool   `json:"valid"`
	Reason string `json:"reason"`
}

func structures(t *testing.T, r Result) []structure {
	t.Helper()
	var out []structure
	for _, f := range r.Facts {
		if f.Kind == "structure" {
			var s structure
			if err := json.Unmarshal(f.Value, &s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
	}
	return out
}

func allValid(t *testing.T, r Result) bool {
	t.Helper()
	ss := structures(t, r)
	for _, s := range ss {
		if !s.Valid {
			return false
		}
	}
	return len(ss) > 0
}

func TestComputePlainFastq(t *testing.T) {
	data := makeFastq(1000)
	res, err := Compute(context.Background(), bytes.NewReader(data), Options{Hashes: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Container != "none" || res.Content != "fastq" || res.Size != int64(len(data)) {
		t.Fatalf("%+v", res)
	}
	if res.Root != blake3.Sum256(data) || res.MD5 != md5.Sum(data) || res.SHA256 != sha256.Sum256(data) {
		t.Fatal("hash mismatch")
	}
	if !allValid(t, res) {
		t.Fatalf("%+v", structures(t, res))
	}
	rec := factValue[struct {
		Records int64 `json:"records"`
		Bases   int64 `json:"bases"`
	}](t, res, "records")
	if rec.Records != 1000 || rec.Bases != 100000 {
		t.Fatalf("%+v", rec)
	}
}

func TestComputeGzipFastq(t *testing.T) {
	raw := makeFastq(2000)
	data := gz(t, raw)
	res, err := Compute(context.Background(), bytes.NewReader(data), Options{Hashes: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Container != "gzip" || res.Content != "fastq" {
		t.Fatalf("%s %s", res.Container, res.Content)
	}
	if res.Root != blake3.Sum256(data) || res.SHA256 != sha256.Sum256(data) {
		t.Fatal("hash must be of the compressed bytes")
	}
	if !allValid(t, res) || len(structures(t, res)) != 2 {
		t.Fatalf("%+v", structures(t, res))
	}
}

func TestComputeReadsSourceExactlyOnce(t *testing.T) {
	raw := makeFastq(30000)
	data := gz(t, raw)
	cr := &countingReader{r: bytes.NewReader(data)}
	res, err := Compute(context.Background(), cr, Options{Hashes: true})
	if err != nil {
		t.Fatal(err)
	}
	if cr.bytes != int64(len(data)) || res.Size != int64(len(data)) {
		t.Fatalf("read %d bytes of %d", cr.bytes, len(data))
	}
}

func TestComputeAcrossBufferBoundaries(t *testing.T) {
	raw := makeFastq(40000)
	if len(raw) < 3*bufferSize {
		t.Fatalf("fixture too small: %d", len(raw))
	}
	for name, data := range map[string][]byte{"plain": raw, "gzip": gz(t, raw)} {
		res, err := Compute(context.Background(), bytes.NewReader(data), Options{Hashes: true})
		if err != nil {
			t.Fatal(name, err)
		}
		if !allValid(t, res) || res.Root != blake3.Sum256(data) {
			t.Fatalf("%s: invalid", name)
		}
	}
}

func TestComputeDetectsTruncationAtTenOffsets(t *testing.T) {
	data := gz(t, makeFastq(3000))
	rng := rand.New(rand.NewSource(9))
	for i := 0; i < 10; i++ {
		cut := 20 + rng.Intn(len(data)-21)
		res, err := Compute(context.Background(), bytes.NewReader(data[:cut]), Options{Hashes: true})
		if err != nil {
			t.Fatal(err)
		}
		if allValid(t, res) {
			t.Fatalf("truncation at %d of %d not detected", cut, len(data))
		}
	}
}

func TestComputeHTMLRenamed(t *testing.T) {
	res, err := Compute(context.Background(), strings.NewReader("<!DOCTYPE html><html>error</html>"), Options{Hashes: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Container != "none" || res.Content != "html" || len(structures(t, res)) != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestComputeEmptyAndTiny(t *testing.T) {
	for _, data := range [][]byte{nil, {'@'}, []byte("x")} {
		res, err := Compute(context.Background(), bytes.NewReader(data), Options{Hashes: true})
		if err != nil {
			t.Fatal(err)
		}
		if res.Root != blake3.Sum256(data) || res.Size != int64(len(data)) {
			t.Fatalf("%q: %+v", data, res)
		}
	}
}

func TestFactsArePureFunctionsOfContent(t *testing.T) {
	data := gz(t, makeFastq(500))
	a, err := Compute(context.Background(), bytes.NewReader(data), Options{Hashes: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Compute(context.Background(), io.MultiReader(bytes.NewReader(data[:100]), bytes.NewReader(data[100:])), Options{Hashes: true})
	if err != nil {
		t.Fatal(err)
	}
	ja, _ := json.Marshal(a.Facts)
	jb, _ := json.Marshal(b.Facts)
	if !bytes.Equal(ja, jb) {
		t.Fatal("facts differ for identical content")
	}
}

func TestComputeWithoutHashesSkipsDigests(t *testing.T) {
	res, err := Compute(context.Background(), bytes.NewReader(makeFastq(10)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Fact("digests"); ok {
		t.Fatal("digests computed without Hashes")
	}
	if !allValid(t, res) {
		t.Fatal("structure not validated")
	}
}

func TestComputeDeclaredFormatOverridesDetection(t *testing.T) {
	res, err := Compute(context.Background(), bytes.NewReader(makeFastq(10)), Options{Format: "fasta"})
	if err != nil {
		t.Fatal(err)
	}
	if allValid(t, res) {
		t.Fatal("fastq parsed as fasta must not be valid")
	}
}

type slowReader struct{ r io.Reader }

func (s slowReader) Read(p []byte) (int, error) {
	time.Sleep(20 * time.Millisecond)
	return s.r.Read(p[:min(len(p), 1<<16)])
}

func TestComputeHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err := Compute(ctx, slowReader{bytes.NewReader(make([]byte, 64<<20))}, Options{Hashes: true})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

type failingReader struct{ n int }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, io.ErrClosedPipe
	}
	k := min(len(p), f.n)
	f.n -= k
	return k, nil
}

func TestComputePropagatesReadErrors(t *testing.T) {
	_, err := Compute(context.Background(), &failingReader{n: 3 * bufferSize}, Options{Hashes: true})
	if err != io.ErrClosedPipe {
		t.Fatalf("err = %v", err)
	}
}
