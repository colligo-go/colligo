package facts

import (
	"context"
	"io"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

type chunk struct {
	buf  []byte
	data []byte
	refs atomic.Int32
}

type consumer func(ctx context.Context, in <-chan *chunk, release func(*chunk)) error

type ring struct {
	free chan *chunk
	outs []chan *chunk
}

func newRing(buffers, size int) *ring {
	r := &ring{free: make(chan *chunk, buffers)}
	for i := 0; i < buffers; i++ {
		r.free <- &chunk{buf: make([]byte, size)}
	}
	return r
}

func (r *ring) release(c *chunk) {
	if c.refs.Add(-1) == 0 {
		r.free <- c
	}
}

func (r *ring) fill(ctx context.Context, src io.Reader) (*chunk, error) {
	select {
	case c := <-r.free:
		n, err := io.ReadFull(src, c.buf)
		c.data = c.buf[:n]
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			err = nil
		}
		return c, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *ring) run(ctx context.Context, src io.Reader, first *chunk, consumers []consumer) (int64, error) {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(len(consumers) + 1)
	r.outs = make([]chan *chunk, len(consumers))
	for i := range r.outs {
		r.outs[i] = make(chan *chunk, cap(r.free))
	}
	for i, fn := range consumers {
		in := r.outs[i]
		g.Go(func() error { return fn(ctx, in, r.release) })
	}
	var total int64
	g.Go(func() error {
		defer func() {
			for _, out := range r.outs {
				close(out)
			}
		}()
		c := first
		for {
			total += int64(len(c.data))
			if len(c.data) > 0 && len(consumers) > 0 {
				c.refs.Store(int32(len(consumers)))
				for _, out := range r.outs {
					out <- c
				}
			} else {
				r.free <- c
			}
			if len(c.data) < len(c.buf) {
				return nil
			}
			next, err := r.fill(ctx, src)
			if err != nil {
				return err
			}
			c = next
		}
	})
	err := g.Wait()
	return total, err
}

func drain(in <-chan *chunk, release func(*chunk)) {
	for c := range in {
		release(c)
	}
}
