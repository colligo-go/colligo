// Package transport fetches metadata over HTTP. It is the only package that
// reveals the real value of a secret.Endpoint, and it never reads a body
// without a size bound.
package transport

import (
	"context"
	"io"

	"github.com/colligo-go/colligo/secret"
)

// Transport is the network seam of the adapters. A fake implementation in a
// test replaces the HTTP client without touching the adapter.
type Transport interface {
	Get(ctx context.Context, endpoint secret.Endpoint, rng *ByteRange) (*Response, error)
}

// ByteRange is reserved for the resume of Slice 3. Get accepts only nil.
type ByteRange struct {
	Start int64
	End   int64
}

// Response carries a successful answer. Body is bounded by the client's
// size limit and must be closed by the caller. ContentLength is -1 when the
// server did not declare it.
type Response struct {
	Status        int
	ContentType   string
	ContentLength int64
	Body          io.ReadCloser
}
