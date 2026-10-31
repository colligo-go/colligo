package transport

import (
	"context"
	"io"

	"github.com/colligo-go/colligo/secret"
)

// ByteRange is an inclusive range of bytes. The metadata adapters always pass nil.
type ByteRange struct {
	Start int64
	End   int64
}

// Response carries the body unread: the caller bounds how much of it to read and closes it.
type Response struct {
	StatusCode  int
	ContentType string
	Body        io.ReadCloser
}

type Transport interface {
	Get(ctx context.Context, url secret.Endpoint, rng *ByteRange) (*Response, error)
}
