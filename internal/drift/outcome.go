package drift

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/source"
	"github.com/colligo-go/colligo/transport"
)

const (
	expectedStatus      = 200
	expectedContentType = "text/plain"
)

// Outcome is what the adapter made of a response, without any value: whether
// it parsed, why it did not, and the codes of the defects it reported.
type Outcome struct {
	Result string
	Codes  []string
}

// Observe summarises the result of source.Fetch.
func Observe(fetched source.Fetched, err error) Outcome {
	outcome := Outcome{Result: "ok"}
	var notFound *model.NotFoundError
	var failure *model.SourceError
	switch {
	case errors.As(err, &notFound):
		outcome.Result = "not_found"
	case errors.As(err, &failure):
		outcome.Result = "error:" + string(failure.Kind)
		if cause := driftCause(err); cause != "" {
			outcome.Result += " (" + cause + ")"
		}
	case err != nil:
		outcome.Result = "error:other"
	}
	for _, issue := range fetched.Issues {
		if !slices.Contains(outcome.Codes, issue.Code) {
			outcome.Codes = append(outcome.Codes, issue.Code)
		}
	}
	slices.Sort(outcome.Codes)
	return outcome
}

// IsOutage reports whether an adapter error says the source was unreachable
// or unwell, which is not drift and must not open a drift issue.
func IsOutage(err error) bool {
	var failure *model.SourceError
	if !errors.As(err, &failure) || driftCause(err) != "" {
		return false
	}
	return failure.Kind == model.SourceUnavailable || failure.Kind == model.SourceUnexpectedStatus
}

func driftCause(err error) string {
	var contentType *transport.ContentTypeError
	var notFound *transport.NotFoundError
	var tooLarge *transport.ResponseTooLargeError
	switch {
	case errors.As(err, &contentType):
		return "content type refused"
	case errors.As(err, &notFound):
		return "http not found"
	case errors.As(err, &tooLarge):
		return "response too large"
	}
	return ""
}

// DiffOutcome lists how the live outcome differs from the baseline one.
func DiffOutcome(baseline, live Outcome) []string {
	var lines []string
	if baseline.Result != live.Result {
		lines = append(lines, fmt.Sprintf("adapter: result was %s, is now %s", baseline.Result, live.Result))
	}
	if !slices.Equal(baseline.Codes, live.Codes) {
		lines = append(lines, fmt.Sprintf("adapter: defect codes were [%s], are now [%s]", strings.Join(baseline.Codes, " "), strings.Join(live.Codes, " ")))
	}
	return lines
}

// DiffEnvelope compares the HTTP status and media type of a live response
// with the ones the adapter was written against.
func DiffEnvelope(raw model.RawResponse) []string {
	var lines []string
	if raw.StatusCode != expectedStatus {
		lines = append(lines, fmt.Sprintf("http: status was %d, is now %d", expectedStatus, raw.StatusCode))
	}
	media, _, _ := strings.Cut(raw.ContentType, ";")
	if strings.TrimSpace(strings.ToLower(media)) != expectedContentType {
		lines = append(lines, fmt.Sprintf("http: content type was %s, is now %s", expectedContentType, strings.TrimSpace(media)))
	}
	return lines
}

// Replay is a transport that answers every request with a recorded body.
type Replay struct{ Body []byte }

var _ transport.Transport = Replay{}

// Get returns the recorded body as a 200 text/plain response.
func (r Replay) Get(_ context.Context, _ secret.Endpoint, _ *transport.ByteRange) (*transport.Response, error) {
	return &transport.Response{
		Status:        expectedStatus,
		ContentType:   expectedContentType,
		ContentLength: int64(len(r.Body)),
		Body:          io.NopCloser(strings.NewReader(string(r.Body))),
	}, nil
}
