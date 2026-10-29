package facts

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"testing/iotest"
	"time"

	"go.uber.org/goleak"
	"golang.org/x/sync/errgroup"
	"lukechampine.com/blake3"
)

var errInjected = errors.New("injected read failure")

type failAt struct {
	r    io.Reader
	left int
}

func (f *failAt) Read(p []byte) (int, error) {
	if f.left == 0 {
		return 0, errInjected
	}
	n, err := f.r.Read(p[:min(len(p), f.left)])
	f.left -= n
	return n, err
}

type onProgress struct {
	r     io.Reader
	total int
	at    int
	fire  func()
	fired bool
}

func (o *onProgress) Read(p []byte) (int, error) {
	n, err := o.r.Read(p)
	o.total += n
	if !o.fired && o.total >= o.at {
		o.fired = true
		o.fire()
	}
	return n, err
}

func randomBytes(seed int64, n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(b)
	return b
}

func incompressibleFastq(records int) []byte {
	rng := rand.New(rand.NewSource(11))
	var b bytes.Buffer
	for i := 0; i < records; i++ {
		seq, qual := make([]byte, 100), make([]byte, 100)
		for j := range seq {
			seq[j] = "ACGT"[rng.Intn(4)]
			qual[j] = byte('#' + rng.Intn(40))
		}
		fmt.Fprintf(&b, "@R.%d/1\n%s\n+\n%s\n", i+1, seq, qual)
	}
	return b.Bytes()
}

func TestComputeReaderFailureAtEveryRelevantPosition(t *testing.T) {
	plain := incompressibleFastq(12000)
	inputs := map[string][]byte{
		"opaque": randomBytes(1, 2*bufferSize+100),
		"plain":  plain,
		"gzip":   gz(t, plain),
		"bgzf":   bgzf(t, plain, true),
	}
	for name, data := range inputs {
		positions := map[string]int{
			"before the first byte":      0,
			"one byte in":                1,
			"middle of the first buffer": bufferSize / 2,
			"one byte before 1 MiB":      bufferSize - 1,
			"exactly 1 MiB":              bufferSize,
			"one byte after 1 MiB":       bufferSize + 1,
			"middle of the stream":       len(data) / 2,
			"after the last byte":        len(data),
		}
		for label, at := range positions {
			t.Run(name+"/"+label, func(t *testing.T) {
				defer goleak.VerifyNone(t)
				src := &failAt{r: bytes.NewReader(data), left: at}
				_, err := Compute(context.Background(), src, Options{Hashes: true})
				if !errors.Is(err, errInjected) {
					t.Fatalf("err = %v", err)
				}
			})
		}
	}
}

func TestComputeCancellationAtEveryStage(t *testing.T) {
	cases := []struct {
		name  string
		data  []byte
		opts  Options
		after int
	}{
		{"during the tree", randomBytes(2, 4*bufferSize), Options{Hashes: true}, bufferSize},
		{"during the inflate", gz(t, randomBytes(6, 4*bufferSize)), Options{}, bufferSize},
		{"during tree and inflate", gz(t, randomBytes(7, 4*bufferSize)), Options{Hashes: true}, bufferSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			if len(tc.data) < tc.after+2*bufferSize {
				t.Fatalf("fixture of %d bytes is too small to cancel at %d", len(tc.data), tc.after)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			src := &onProgress{r: bytes.NewReader(tc.data), at: tc.after, fire: cancel}
			_, err := Compute(ctx, src, tc.opts)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestComputeCancelledBeforeTheFirstByte(t *testing.T) {
	for _, size := range []int{0, 10, bufferSize, 3 * bufferSize} {
		data := randomBytes(3, size)
		for run := 0; run < 20; run++ {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			cr := &countingReader{r: bytes.NewReader(data)}
			_, err := Compute(ctx, cr, Options{Hashes: true})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("size %d run %d: err = %v", size, run, err)
			}
			if cr.calls != 0 {
				t.Fatalf("size %d run %d: source read %d times after cancellation", size, run, cr.calls)
			}
		}
	}
}

func TestComputeCancelledByDeadline(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := Compute(ctx, slowReader{bytes.NewReader(make([]byte, 64<<20))}, Options{Hashes: true})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestEightConcurrentComputesDoNotInterfere(t *testing.T) {
	defer goleak.VerifyNone(t)
	plain := incompressibleFastq(8000)
	inputs := [][]byte{
		plain, gz(t, plain), bgzf(t, plain, true), randomBytes(4, 2*bufferSize+17),
		makeFastq(500), gz(t, makeFastq(900)), randomBytes(5, 3<<20), nil,
	}
	want := make([]Result, len(inputs))
	for i, data := range inputs {
		want[i] = computeBytes(t, data)
	}
	got := make([]Result, len(inputs))
	var g errgroup.Group
	for i, data := range inputs {
		g.Go(func() error {
			res, err := Compute(context.Background(), bytes.NewReader(data), Options{Hashes: true})
			got[i] = res
			return err
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}
	for i := range inputs {
		if got[i].Root != blake3.Sum256(inputs[i]) {
			t.Fatalf("input %d: wrong root", i)
		}
		a, _ := json.Marshal(want[i])
		b, _ := json.Marshal(got[i])
		if !bytes.Equal(a, b) {
			t.Fatalf("input %d: concurrent result differs from the sequential one", i)
		}
	}
}

func TestEightConcurrentComputesOfTheSameBytes(t *testing.T) {
	data := gz(t, incompressibleFastq(8000))
	want := computeBytes(t, data)
	results := make([]Result, 8)
	var g errgroup.Group
	for i := range results {
		g.Go(func() error {
			res, err := Compute(context.Background(), bytes.NewReader(data), Options{Hashes: true})
			results[i] = res
			return err
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(want)
	for i, res := range results {
		if got, _ := json.Marshal(res); !bytes.Equal(got, wantJSON) {
			t.Fatalf("run %d differs", i)
		}
	}
}

func sizedFasta(size int) []byte {
	const header = ">s\n"
	data := make([]byte, 0, size)
	data = append(data, header...)
	for len(data) < size-1 {
		data = append(data, "ACGT"[len(data)%4])
	}
	return append(data, '\n')
}

func TestComputeAtBufferAndGroupBoundaries(t *testing.T) {
	const MiB = 1 << 20
	for _, size := range []int{MiB - 1, MiB, MiB + 1, 8*MiB - 1, 8 * MiB, 8*MiB + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			opaque := randomBytes(int64(size), size)
			res := computeBytes(t, opaque)
			if res.Size != int64(size) || res.Root != blake3.Sum256(opaque) ||
				res.MD5 != md5.Sum(opaque) || res.SHA256 != sha256.Sum256(opaque) {
				t.Fatalf("opaque %d bytes: digests differ from the standard library", size)
			}
			wantGroups := (size + GroupSize - 1) / GroupSize
			if len(res.Groups) != wantGroups {
				t.Fatalf("groups = %d, want %d", len(res.Groups), wantGroups)
			}
			if RootFromGroups(res.Groups) != res.Root {
				t.Fatal("root differs from the one rebuilt from the groups")
			}
			digests := factValue[digestsValue](t, res, "digests")
			if digests.Size != int64(size) || digests.MD5 != res.MD5Hex() || digests.SHA256 != res.SHA256Hex() {
				t.Fatalf("digests fact = %+v", digests)
			}

			fasta := sizedFasta(size)
			res = computeBytes(t, fasta)
			if res.Root != blake3.Sum256(fasta) || res.SHA256 != sha256.Sum256(fasta) || !allValid(t, res) {
				t.Fatalf("fasta %d bytes: %+v", size, structures(t, res))
			}
			rec := factValue[struct {
				Records int64 `json:"records"`
				Bases   int64 `json:"bases"`
			}](t, res, "records")
			if rec.Records != 1 || rec.Bases != int64(size-4) {
				t.Fatalf("fasta records fact = %+v", rec)
			}
		})
	}
}

func factsJSON(t *testing.T, r Result) []byte {
	t.Helper()
	out, err := json.Marshal(r.Facts)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFactsArePureFunctionsOfContentForEveryFormat(t *testing.T) {
	raw := makeFastq(300)
	fasta := []byte(">chr1 first\nACGT\nAC\n>chr2\nGGGG\n")
	gtf := []byte("#!genome-build GRCh38\nchr1\tHAVANA\tgene\t1\t100\t.\t+\t.\tgene_id \"G1\";\n" +
		"chr2\tHAVANA\tgene\t5\t90\t.\t-\t.\tgene_id \"G2\";\n")
	contents := map[string][]byte{
		"fastq": raw,
		"fasta": fasta,
		"gtf":   gtf,
		"gzip":  gz(t, raw),
		"bgzf":  bgzf(t, raw, true),
	}
	names := []string{"a.dat", filepath.Join("deep", "er", "renamed.fastq.gz"), "x"}
	times := []time.Time{time.Unix(0, 0), time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC), time.Now()}
	readers := map[string]func(io.Reader) io.Reader{
		"whole":         func(r io.Reader) io.Reader { return r },
		"one-big":       iotest.HalfReader,
		"eof-with-data": iotest.DataErrReader,
	}
	for label, data := range contents {
		t.Run(label, func(t *testing.T) {
			var first []byte
			var firstRoot [32]byte
			for i, name := range names {
				path := filepath.Join(t.TempDir(), name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, times[i], times[i]); err != nil {
					t.Fatal(err)
				}
				for rname, wrap := range readers {
					f, err := os.Open(path)
					if err != nil {
						t.Fatal(err)
					}
					res, err := Compute(context.Background(), wrap(f), Options{Hashes: true})
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
					got := factsJSON(t, res)
					if first == nil {
						first, firstRoot = got, res.Root
						if len(structures(t, res)) == 0 {
							t.Fatal("fixture produced no structure fact")
						}
						continue
					}
					if !bytes.Equal(first, got) || firstRoot != res.Root {
						t.Fatalf("%s via %s: facts differ for identical content", name, rname)
					}
				}
			}
		})
	}
}
