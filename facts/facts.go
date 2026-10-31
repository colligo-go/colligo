// Package facts reads a file once and derives from that single read the
// BLAKE3 tree, MD5, SHA-256 and the format facts.
package facts

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/klauspost/compress/gzip"
	"io"
	"sort"

	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/model"
)

const (
	bufferSize     = 1 << 20
	bufferCount    = 8
	sniffSize      = 64 << 10
	decompressSize = 256 << 10
)

type Options struct {
	Format string
	Hashes bool
	// Identify adds the identity facts, such as the seqcol digest of a
	// FASTA, to the same read.
	Identify bool
}

type Result struct {
	Size int64
	Root [32]byte
	// Groups holds one chaining value per 8 MiB group. A file of a single
	// group holds the root itself: a lone group is the root node.
	Groups    [][32]byte
	MD5       [16]byte
	SHA256    [32]byte
	Container string
	Content   string
	Facts     []model.Fact
}

func (r Result) RootHex() string   { return hex.EncodeToString(r.Root[:]) }
func (r Result) MD5Hex() string    { return hex.EncodeToString(r.MD5[:]) }
func (r Result) SHA256Hex() string { return hex.EncodeToString(r.SHA256[:]) }

func (r Result) Fact(kind string) (model.Fact, bool) {
	for _, f := range r.Facts {
		if f.Kind == kind {
			return f, true
		}
	}
	return model.Fact{}, false
}

type collector struct {
	facts []model.Fact
}

func (c *collector) add(f []model.Fact) { c.facts = append(c.facts, f...) }

func Compute(ctx context.Context, src io.Reader, opts Options) (Result, error) {
	rg := newRing(bufferCount, bufferSize)
	first, err := rg.fill(ctx, src)
	if err != nil {
		return Result{}, err
	}
	head := first.data[:min(len(first.data), sniffSize)]
	res := Result{Container: format.DetectContainer(head)}

	var (
		tree   treeBuilder
		md5h   = md5.New()
		shah   = sha256.New()
		found  collector
		inner  format.Extractor
		contnr format.Extractor
	)
	if res.Container == format.ContainerNone {
		res.Content = format.DetectContent(head)
		inner = contentExtractor(opts.Format, res.Content, opts.Identify)
	} else if v, ok := format.Lookup(res.Container); ok {
		contnr = v.NewExtractor()
	}

	var consumers []consumer
	if opts.Hashes {
		consumers = append(consumers,
			sink(&tree), sink(md5h), sink(shah))
	}
	switch {
	case contnr != nil:
		consumers = append(consumers, sink(contnr), payload(contnr, opts.Format, opts.Identify, &res, &found))
	case inner != nil:
		consumers = append(consumers, sink(inner))
	}

	size, err := rg.run(ctx, src, first, consumers)
	if err != nil {
		return Result{}, err
	}
	res.Size = size

	if opts.Hashes {
		res.Root, res.Groups = tree.sum()
		copy(res.MD5[:], md5h.Sum(nil))
		copy(res.SHA256[:], shah.Sum(nil))
		digests, err := digestsFact(res)
		if err != nil {
			return Result{}, err
		}
		found.add([]model.Fact{digests})
	}
	for _, e := range []format.Extractor{contnr, inner} {
		if e == nil {
			continue
		}
		fs, err := e.Finish()
		if err != nil {
			return Result{}, err
		}
		found.add(fs)
	}
	res.Facts = found.facts
	sort.SliceStable(res.Facts, func(i, j int) bool {
		a, b := res.Facts[i], res.Facts[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.ExtractorVersion < b.ExtractorVersion
	})
	return res, nil
}

func contentExtractor(declared, detected string, identify bool) format.Extractor {
	name := detected
	if declared != "" {
		name = declared
	}
	v, ok := format.Lookup(name)
	if !ok || v.Compressed() {
		return nil
	}
	return format.ExtractorFor(v, identify)
}

func sink(w io.Writer) consumer {
	return func(ctx context.Context, in <-chan *chunk, release func(*chunk)) error {
		for c := range in {
			_, err := w.Write(c.data)
			release(c)
			if err != nil {
				drain(in, release)
				return err
			}
		}
		return nil
	}
}

type chanReader struct {
	in       <-chan *chunk
	release  func(*chunk)
	cur      *chunk
	off      int
	consumed int64
}

func (r *chanReader) Read(p []byte) (int, error) {
	for r.cur == nil || r.off == len(r.cur.data) {
		if r.cur != nil {
			r.release(r.cur)
			r.cur = nil
		}
		c, ok := <-r.in
		if !ok {
			return 0, io.EOF
		}
		r.cur, r.off = c, 0
	}
	n := copy(p, r.cur.data[r.off:])
	r.off += n
	r.consumed += int64(n)
	return n, nil
}

func (r *chanReader) finish() {
	if r.cur != nil {
		r.release(r.cur)
		r.cur = nil
	}
	drain(r.in, r.release)
}

func payload(container format.Extractor, declared string, identify bool, res *Result, found *collector) consumer {
	return func(ctx context.Context, in <-chan *chunk, release func(*chunk)) error {
		cr := &chanReader{in: in, release: release}
		defer cr.finish()
		var decompressed int64
		report := func(err error) {
			if r, ok := container.(format.PayloadReporter); ok {
				r.ReportPayload(cr.consumed, decompressed, err)
			}
		}
		gz, err := gzip.NewReader(cr)
		if err != nil {
			report(err)
			return nil
		}
		buf := make([]byte, decompressSize)
		filled := 0
		var readErr error
		for filled < sniffSize && readErr == nil {
			var n int
			n, readErr = gz.Read(buf[filled:])
			filled += n
		}
		decompressed = int64(filled)
		content := format.DetectContent(buf[:filled])
		res.Content = content
		inner := contentExtractor(declared, content, identify)
		if inner != nil {
			inner.Write(buf[:filled])
		}
		for readErr == nil {
			var n int
			n, readErr = gz.Read(buf)
			decompressed += int64(n)
			if inner != nil && n > 0 {
				inner.Write(buf[:n])
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			report(nil)
		} else {
			report(readErr)
		}
		if inner != nil {
			fs, err := inner.Finish()
			if err != nil {
				return err
			}
			found.add(fs)
		}
		return nil
	}
}

type digestsValue struct {
	MD5    string `json:"md5"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func digestsFact(r Result) (model.Fact, error) {
	return jsonFact("digests", "digests-1", digestsValue{MD5: r.MD5Hex(), SHA256: r.SHA256Hex(), Size: r.Size})
}

func jsonFact(kind, version string, v any) (model.Fact, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return model.Fact{}, err
	}
	return model.Fact{Kind: kind, ExtractorVersion: version, Value: raw}, nil
}
