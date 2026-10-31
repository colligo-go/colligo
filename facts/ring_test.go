package facts

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func requireAllBuffersReturned(t *testing.T, r *ring, buffers int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for len(r.free) != buffers {
		select {
		case <-deadline:
			t.Fatalf("%d of %d buffers returned to the pool", len(r.free), buffers)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestDrainReturnsEveryQueuedBufferToThePool(t *testing.T) {
	const buffers = 4
	r := newRing(buffers, 16)
	in := make(chan *chunk, buffers)
	for range buffers {
		c := <-r.free
		c.refs.Store(1)
		in <- c
	}
	close(in)
	if len(r.free) != 0 {
		t.Fatalf("setup: %d buffers still free", len(r.free))
	}
	drain(in, r.release)
	if len(r.free) != buffers {
		t.Fatalf("%d of %d buffers returned", len(r.free), buffers)
	}
}

func TestDrainOfASharedBufferWaitsForTheLastConsumer(t *testing.T) {
	r := newRing(1, 16)
	c := <-r.free
	c.refs.Store(2)
	in := make(chan *chunk, 1)
	in <- c
	close(in)
	drain(in, r.release)
	if len(r.free) != 0 {
		t.Fatal("the buffer went back while another consumer still held it")
	}
	r.release(c)
	if len(r.free) != 1 {
		t.Fatal("the buffer did not go back after the last release")
	}
}

type stallingFailure struct {
	wait time.Duration
	err  error
}

func (w *stallingFailure) Write([]byte) (int, error) {
	time.Sleep(w.wait)
	return 0, w.err
}

func TestConsumerThatFailsEarlyOnALargeFileDoesNotStallTheRead(t *testing.T) {
	const buffers, size = 4, 1024
	failure := errors.New("consumer stopped")
	src := bytes.NewReader(bytes.Repeat([]byte("x"), 200*size))
	r := newRing(buffers, size)
	first, err := r.fill(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}

	var healthy int64
	stopsEarly := sink(&stallingFailure{wait: 50 * time.Millisecond, err: failure})
	counts := func(_ context.Context, in <-chan *chunk, release func(*chunk)) error {
		for c := range in {
			healthy += int64(len(c.data))
			release(c)
		}
		return nil
	}

	done := make(chan error, 1)
	go func() {
		_, err := r.run(context.Background(), src, first, []consumer{stopsEarly, counts})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, failure) {
			t.Fatalf("error %v, want the consumer's", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the read stalled after a consumer stopped")
	}
	requireAllBuffersReturned(t, r, buffers)
}

func TestCancellationWhileConsumersHoldBuffersReturnsThemAll(t *testing.T) {
	const buffers, size = 4, 1024
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := newRing(buffers, size)
	src := bytes.NewReader(bytes.Repeat([]byte("y"), 200*size))
	first, err := r.fill(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	consumeThenCancel := func(_ context.Context, in <-chan *chunk, release func(*chunk)) error {
		for c := range in {
			release(c)
			seen++
			if seen == 2 {
				cancel()
			}
		}
		return nil
	}
	_, err = r.run(ctx, src, first, []consumer{consumeThenCancel})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v", err)
	}
	requireAllBuffersReturned(t, r, buffers)
}
