package model

import "fmt"

type SourceErrorKind string

const (
	SourceInvalidAccession SourceErrorKind = "invalid_accession"
	SourceNoTransport      SourceErrorKind = "no_transport"
	SourceUnavailable      SourceErrorKind = "unavailable"
	SourceUnexpectedStatus SourceErrorKind = "unexpected_status"
	SourceTooLarge         SourceErrorKind = "too_large"
	SourceEmptyResponse    SourceErrorKind = "empty_response"
	SourceNotTabular       SourceErrorKind = "not_tabular"
	SourceMissingColumn    SourceErrorKind = "missing_column"
	SourceNoUsableRuns     SourceErrorKind = "no_usable_runs"
)

// SourceError reports a response that cannot be trusted. Detail never carries
// a URL, a path or any part of the response body.
type SourceError struct {
	Source string
	Kind   SourceErrorKind
	Detail string
	Err    error
}

func (e *SourceError) Error() string {
	msg := fmt.Sprintf("source %s: %s: %s", e.Source, e.Kind, e.Detail)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *SourceError) Unwrap() error { return e.Err }

func (e *SourceError) Severity() Severity { return Blocking }

type NotFoundError struct {
	Source    string
	Accession string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("source %s: accession %s not found", e.Source, e.Accession)
}

func (e *NotFoundError) Severity() Severity { return Blocking }
