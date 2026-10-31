package transport

import (
	"fmt"
	"time"

	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
)

type NotFoundError struct {
	Endpoint secret.Endpoint
	Status   int
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s: not found (status %d)", e.Endpoint, e.Status)
}

func (*NotFoundError) Severity() model.Severity { return model.Blocking }

type AuthError struct {
	Endpoint secret.Endpoint
	Status   int
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("%s: access refused (status %d); check the credential", e.Endpoint, e.Status)
}

func (*AuthError) Severity() model.Severity { return model.Blocking }

// StatusError is an HTTP status the client does not retry, or the last one
// seen when retries ran out. ContentType is the media type of the answer, so
// an HTML error page behind a 5xx is visible to the caller.
type StatusError struct {
	Endpoint    secret.Endpoint
	Status      int
	ContentType string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s: unexpected status %d (content type %q)", e.Endpoint, e.Status, e.ContentType)
}

func (*StatusError) Severity() model.Severity { return model.Blocking }

// ContentTypeError reports a 2xx answer whose media type is not in the
// allowed list, typically an HTML page served where a TSV was expected.
type ContentTypeError struct {
	Endpoint secret.Endpoint
	Got      string
}

func (e *ContentTypeError) Error() string {
	got := e.Got
	if got == "" {
		got = "none"
	}
	return fmt.Sprintf("%s: unexpected content type %s", e.Endpoint, got)
}

func (*ContentTypeError) Severity() model.Severity { return model.Blocking }

type ResponseTooLargeError struct {
	Endpoint secret.Endpoint
	Limit    int64
}

func (e *ResponseTooLargeError) Error() string {
	return fmt.Sprintf("%s: response larger than the %d byte limit", e.Endpoint, e.Limit)
}

func (*ResponseTooLargeError) Severity() model.Severity { return model.Blocking }

// RetryAfterError reports a Retry-After above the backoff cap. The client
// neither waits that long nor retries sooner than the server asked.
type RetryAfterError struct {
	Endpoint secret.Endpoint
	After    time.Duration
	Cap      time.Duration
}

func (e *RetryAfterError) Error() string {
	return fmt.Sprintf("%s: server asked to wait %s, above the %s cap", e.Endpoint, e.After, e.Cap)
}

func (*RetryAfterError) Severity() model.Severity { return model.Blocking }

type RetryExhaustedError struct {
	Endpoint secret.Endpoint
	Attempts int
	Err      error
}

func (e *RetryExhaustedError) Error() string {
	return fmt.Sprintf("%s: gave up after %d attempts: %v", e.Endpoint, e.Attempts, e.Err)
}

func (e *RetryExhaustedError) Unwrap() error { return e.Err }

func (*RetryExhaustedError) Severity() model.Severity { return model.Blocking }

type RangeNotImplementedError struct{}

func (*RangeNotImplementedError) Error() string {
	return "byte ranges arrive with the resume slice; pass a nil range"
}

func (*RangeNotImplementedError) Severity() model.Severity { return model.Fatal }

type UnsupportedEndpointError struct {
	Endpoint secret.Endpoint
}

func (e *UnsupportedEndpointError) Error() string {
	return fmt.Sprintf("%s: only http and https endpoints are supported", e.Endpoint)
}

func (*UnsupportedEndpointError) Severity() model.Severity { return model.Fatal }
