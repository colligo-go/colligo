package verifier

import (
	"sync/atomic"

	"github.com/colligo-go/colligo/internal/fsaccess"
	"github.com/colligo-go/colligo/secret"
)

// Progress counts the bytes read from the files of a check, for a renderer
// to show. The callback runs on the goroutines that read, so it must be quick
// and safe for concurrent use. A nil *Progress is valid and does nothing.
type Progress struct {
	total    atomic.Int64
	done     atomic.Int64
	onChange func(done, total int64)
}

func NewProgress(onChange func(done, total int64)) *Progress {
	return &Progress{onChange: onChange}
}

func (p *Progress) Total() int64 {
	if p == nil {
		return 0
	}
	return p.total.Load()
}

func (p *Progress) Done() int64 {
	if p == nil {
		return 0
	}
	return p.done.Load()
}

func (p *Progress) expect(names []secret.Name) {
	if p == nil {
		return
	}
	for _, n := range names {
		if info, err := fsaccess.Stat(n.Raw); err == nil && info.Mode().IsRegular() {
			p.total.Add(info.Size())
		}
	}
	p.changed()
}

func (p *Progress) advance(n int) {
	if p == nil || n <= 0 {
		return
	}
	p.done.Add(int64(n))
	p.changed()
}

func (p *Progress) changed() {
	if p.onChange != nil {
		p.onChange(p.done.Load(), p.total.Load())
	}
}

type countingSource struct {
	readSource
	progress *Progress
}

func (c countingSource) Read(buf []byte) (int, error) {
	n, err := c.readSource.Read(buf)
	c.progress.advance(n)
	return n, err
}
