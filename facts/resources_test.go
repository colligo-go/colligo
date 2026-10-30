package facts

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"io"
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/gzip"
	"lukechampine.com/blake3"
)

const (
	zeroFileSize    = 2 << 30
	peakHeapCeiling = 32 << 20
)

type heapSampler struct {
	stop chan struct{}
	done sync.WaitGroup
	base uint64
	peak uint64
	log  []uint64
}

func startHeapSampler(every time.Duration) *heapSampler {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	s := &heapSampler{stop: make(chan struct{}), base: m.HeapAlloc}
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		tick := time.NewTicker(every)
		defer tick.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-tick.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				s.log = append(s.log, m.HeapAlloc)
				s.peak = max(s.peak, m.HeapAlloc)
			}
		}
	}()
	return s
}

func (s *heapSampler) finish() (peakAboveBase uint64) {
	close(s.stop)
	s.done.Wait()
	if s.peak < s.base {
		return 0
	}
	return s.peak - s.base
}

func zeroGzipStream(t *testing.T, total int64) io.Reader {
	t.Helper()
	const segment = 1 << 20
	zeros := make([]byte, segment)
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	var out [3][]byte
	for i := range out {
		if _, err := zw.Write(zeros); err != nil {
			t.Fatal(err)
		}
		if err := zw.Flush(); err != nil {
			t.Fatal(err)
		}
		out[i] = bytes.Clone(buf.Bytes())
		buf.Reset()
	}
	if !bytes.Equal(out[1], out[2]) {
		t.Fatal("the compressor output for a segment of zeros is not periodic; the stream cannot be replayed")
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	closing := buf.Bytes()
	var crc uint32
	for written := int64(0); written < total; written += segment {
		crc = crc32.Update(crc, crc32.IEEETable, zeros)
	}
	trailer := binary.LittleEndian.AppendUint32(nil, crc)
	trailer = binary.LittleEndian.AppendUint32(trailer, uint32(total))
	tail := append(bytes.Clone(closing[:len(closing)-8]), trailer...)

	parts := [][]byte{out[0]}
	for i := int64(1); i < total/segment; i++ {
		parts = append(parts, out[1])
	}
	parts = append(parts, tail)
	return &concatReader{parts: parts}
}

type concatReader struct {
	parts [][]byte
}

func (c *concatReader) Read(p []byte) (int, error) {
	for len(c.parts) > 0 && len(c.parts[0]) == 0 {
		c.parts = c.parts[1:]
	}
	if len(c.parts) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.parts[0])
	c.parts[0] = c.parts[0][n:]
	return n, nil
}

type uncompressedBytes struct {
	Valid        bool  `json:"valid"`
	Uncompressed int64 `json:"uncompressed_bytes"`
}

func TestComputeOfAHugelyCompressedGzipStaysUnderTheMemoryCeiling(t *testing.T) {
	sum := md5.New()
	src := &countingReader{r: io.TeeReader(zeroGzipStream(t, zeroFileSize), sum)}
	sampler := startHeapSampler(2 * time.Millisecond)
	start := time.Now()
	res, err := Compute(context.Background(), src, Options{Hashes: true})
	peak := sampler.finish()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d compressed bytes for %d MiB of zeros (ratio %d:1) in %v; peak heap %.1f MiB above baseline",
		src.bytes, zeroFileSize>>20, int64(zeroFileSize)/src.bytes, time.Since(start), float64(peak)/(1<<20))

	if res.Container != "gzip" || res.Size != src.bytes || res.MD5Hex() != hex.EncodeToString(sum.Sum(nil)) {
		t.Fatalf("container %q size %d (read %d) md5 %s", res.Container, res.Size, src.bytes, res.MD5Hex())
	}
	got := factValue[uncompressedBytes](t, res, "structure")
	if !got.Valid || got.Uncompressed != zeroFileSize {
		t.Fatalf("structure %+v, want valid with %d uncompressed bytes", got, int64(zeroFileSize))
	}
	if peak > peakHeapCeiling {
		t.Fatalf("peak heap %d MiB above baseline, ceiling is %d MiB", peak>>20, peakHeapCeiling>>20)
	}
}

type shortReads struct {
	total   int
	next    int
	pending []byte
	seqs    [shortReadKinds][]byte
	quals   [shortReadKinds][]byte
}

const (
	shortReadKinds    = 5
	shortReadMinBases = 20
)

func newShortReads(total int) *shortReads {
	r := &shortReads{total: total}
	for k := range r.seqs {
		r.seqs[k] = bytes.Repeat([]byte("ACGT"), 8)[:shortReadMinBases+k]
		r.quals[k] = bytes.Repeat([]byte("I"), shortReadMinBases+k)
	}
	return r
}

func (r *shortReads) Read(p []byte) (int, error) {
	for len(r.pending) < len(p) && r.next < r.total {
		k := r.next % shortReadKinds
		r.next++
		r.pending = append(r.pending, "@R."...)
		r.pending = strconv.AppendInt(r.pending, int64(r.next), 10)
		r.pending = append(r.pending, "/1\n"...)
		r.pending = append(r.pending, r.seqs[k]...)
		r.pending = append(r.pending, "\n+\n"...)
		r.pending = append(r.pending, r.quals[k]...)
		r.pending = append(r.pending, '\n')
	}
	if len(r.pending) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.pending)
	r.pending = r.pending[:copy(r.pending, r.pending[n:])]
	return n, nil
}

type recordCounts struct {
	Records  int64         `json:"records"`
	Bases    int64         `json:"bases"`
	Lengths  map[int]int64 `json:"length_histogram"`
	Overflow int64         `json:"length_overflow"`
}

type readEnds struct {
	First string `json:"first"`
	Last  string `json:"last"`
}

func TestComputeOfTwentyMillionShortReadsKeepsFactsAndMemoryStable(t *testing.T) {
	if os.Getenv("COLLIGO_SLOW") == "" {
		t.Skip("set COLLIGO_SLOW=1 to run the 20-million-read stream; it takes minutes under -race")
	}
	const reads = 20_000_000
	sum := md5.New()
	src := &countingReader{r: io.TeeReader(newShortReads(reads), sum)}
	sampler := startHeapSampler(20 * time.Millisecond)
	start := time.Now()
	res, err := Compute(context.Background(), src, Options{Hashes: true})
	peak := sampler.finish()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d MiB in %v; peak heap %.1f MiB above baseline", src.bytes>>20, time.Since(start), float64(peak)/(1<<20))

	if res.Size != src.bytes || res.MD5Hex() != hex.EncodeToString(sum.Sum(nil)) {
		t.Fatalf("size %d (read %d), md5 %s", res.Size, src.bytes, res.MD5Hex())
	}
	counts := factValue[recordCounts](t, res, "records")
	perLength := int64(reads / shortReadKinds)
	wantBases := int64(0)
	for k := 0; k < shortReadKinds; k++ {
		wantBases += perLength * int64(shortReadMinBases+k)
		if counts.Lengths[shortReadMinBases+k] != perLength {
			t.Errorf("length %d: %d reads, want %d", shortReadMinBases+k, counts.Lengths[shortReadMinBases+k], perLength)
		}
	}
	if counts.Records != reads || counts.Bases != wantBases || len(counts.Lengths) != shortReadKinds || counts.Overflow != 0 {
		t.Errorf("records %d bases %d (want %d) buckets %d overflow %d", counts.Records, counts.Bases, wantBases, len(counts.Lengths), counts.Overflow)
	}
	ends := factValue[readEnds](t, res, "read_ends")
	if ends.First != "R.1" || ends.Last != "R."+strconv.Itoa(reads) {
		t.Errorf("read_ends %+v", ends)
	}

	if peak > peakHeapCeiling {
		t.Errorf("peak heap %d MiB above baseline, ceiling is %d MiB", peak>>20, peakHeapCeiling>>20)
	}
	quarter := len(sampler.log) / 4
	if quarter == 0 {
		t.Fatal("too few heap samples to compare the start and the end")
	}
	early, late := slicePeak(sampler.log[:quarter]), slicePeak(sampler.log[len(sampler.log)-quarter:])
	if late > early+8<<20 {
		t.Errorf("heap grew from %d MiB in the first quarter to %d MiB in the last", early>>20, late>>20)
	}
}

func slicePeak(samples []uint64) uint64 {
	var peak uint64
	for _, v := range samples {
		peak = max(peak, v)
	}
	return peak
}

func changingFile(t *testing.T, size int, change func(path string)) (res Result, read []byte, original []byte, path string) {
	t.Helper()
	original = randomBytes(31, size)
	path = t.TempDir() + "/changing.bin"
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var seen bytes.Buffer
	src := &onProgress{r: io.TeeReader(f, &seen), at: size / 2, fire: func() { change(path) }}
	res, err = Compute(context.Background(), src, Options{Hashes: true})
	if err != nil {
		t.Fatal(err)
	}
	if !src.fired {
		t.Fatal("the file was never changed during the read")
	}
	return res, seen.Bytes(), original, path
}

func requireDescribes(t *testing.T, res Result, read []byte) {
	t.Helper()
	md5sum := md5.Sum(read)
	if res.Size != int64(len(read)) || res.MD5 != md5sum || res.Root != blake3.Sum256(read) {
		t.Fatalf("result describes %d bytes, the reader returned %d", res.Size, len(read))
	}
}

func TestFileTruncatedDuringTheReadIsDescribedByTheBytesActuallyRead(t *testing.T) {
	const size, kept = 6 << 20, 3 << 20
	res, read, original, _ := changingFile(t, size, func(path string) {
		if err := os.Truncate(path, kept); err != nil {
			t.Error(err)
		}
	})
	if len(read) >= size || len(read) < kept {
		t.Fatalf("read %d bytes of a file cut from %d to %d", len(read), size, kept)
	}
	if !bytes.Equal(read, original[:len(read)]) {
		t.Fatal("the bytes read are not a prefix of the original")
	}
	requireDescribes(t, res, read)
}

func TestFileGrownDuringTheReadIsDescribedByTheBytesActuallyRead(t *testing.T) {
	const size, extra = 4 << 20, 2 << 20
	appended := randomBytes(32, extra)
	res, read, original, path := changingFile(t, size, func(path string) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		if _, err := f.Write(appended); err != nil {
			t.Error(err)
		}
	})
	if want := append(bytes.Clone(original), appended...); !bytes.Equal(read, want) {
		t.Fatalf("read %d bytes, the grown file holds %d", len(read), len(want))
	}
	requireDescribes(t, res, read)
	if st, err := os.Stat(path); err != nil || st.Size() != int64(size+extra) {
		t.Fatalf("stat %v %v", st, err)
	}
}

type workload struct {
	name string
	data func(records int) []byte
}

func allocationsOf(t *testing.T, data []byte) (mallocs, bytesAllocated int64) {
	t.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := Compute(context.Background(), bytes.NewReader(data), Options{Hashes: true}); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	return int64(after.Mallocs - before.Mallocs), int64(after.TotalAlloc - before.TotalAlloc)
}

func TestComputeAllocationsPerMiBStayWithinTheLimit(t *testing.T) {
	const (
		maxMallocsPerMiB = 8
		maxBytesPerMiB   = 16 << 10
	)
	workloads := []workload{
		{"plain fastq", func(n int) []byte { return makeFastq(n) }},
		{"gzip fastq", func(n int) []byte { return gz(t, incompressibleFastq(n)) }},
	}
	for _, w := range workloads {
		small, large := w.data(8_000), w.data(40_000)
		allocationsOf(t, small)
		smallMallocs, smallBytes := allocationsOf(t, small)
		largeMallocs, largeBytes := allocationsOf(t, large)
		mib := float64(len(large)-len(small)) / (1 << 20)
		mallocsPerMiB := float64(largeMallocs-smallMallocs) / mib
		bytesPerMiB := float64(largeBytes-smallBytes) / mib
		t.Logf("%s: %.2f allocations and %.0f bytes allocated per extra MiB read (%.1f MiB more)", w.name, mallocsPerMiB, bytesPerMiB, mib)
		if mallocsPerMiB > maxMallocsPerMiB || bytesPerMiB > maxBytesPerMiB {
			t.Errorf("%s: %.2f allocations and %.0f bytes per MiB, limits are %d and %d", w.name, mallocsPerMiB, bytesPerMiB, maxMallocsPerMiB, maxBytesPerMiB)
		}
	}
}
