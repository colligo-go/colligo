package ena

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/source"
	"github.com/colligo-go/colligo/transport"
)

const (
	sourceID           = "ena"
	defaultBase        = "https://www.ebi.ac.uk/ena/portal/api"
	defaultMaxResponse = 16 << 20
)

var accessionPattern = regexp.MustCompile(`^[A-Z]{2,6}[0-9]+$`)

type Source struct {
	transport   transport.Transport
	base        string
	maxResponse int64
	now         func() time.Time
}

type Option func(*Source)

func WithTransport(t transport.Transport) Option {
	return func(s *Source) { s.transport = t }
}

func WithBaseURL(base secret.Endpoint) Option {
	return func(s *Source) { s.base = strings.TrimRight(base.String(), "/") }
}

// WithMaxResponseBytes bounds how much of a response is read into memory.
func WithMaxResponseBytes(n int64) Option {
	return func(s *Source) { s.maxResponse = n }
}

func WithClock(now func() time.Time) Option {
	return func(s *Source) { s.now = now }
}

// New returns an adapter that does nothing until WithTransport is given.
func New(opts ...Option) *Source {
	s := &Source{base: defaultBase, maxResponse: defaultMaxResponse, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

var _ source.Source = (*Source)(nil)

func (s *Source) ID() string { return sourceID }

func (s *Source) Fetch(ctx context.Context, accession string) (source.Fetched, error) {
	fetched := source.Fetched{Experiment: model.Experiment{ID: accession, Source: sourceID}}
	if !accessionPattern.MatchString(accession) {
		return fetched, &model.SourceError{Source: sourceID, Kind: model.SourceInvalidAccession, Detail: "the accession does not look like an ENA accession"}
	}
	if s.transport == nil {
		return fetched, &model.SourceError{Source: sourceID, Kind: model.SourceNoTransport, Detail: "the adapter was built without a transport"}
	}
	endpoint, err := secret.NewEndpoint(s.reportURL(accession))
	if err != nil {
		return fetched, &model.SourceError{Source: sourceID, Kind: model.SourceInvalidAccession, Detail: "the request URL cannot be built", Err: err}
	}

	raw, err := s.get(ctx, endpoint)
	if raw != nil {
		fetched.Raw = append(fetched.Raw, *raw)
	}
	if err != nil {
		return fetched, err
	}

	runs, issues, err := parseReport(raw.Body)
	if err != nil {
		return fetched, err
	}
	fetched.Issues = issues
	for _, r := range runs {
		fetched.Samples = append(fetched.Samples, model.Sample{
			ID:           r.accession,
			ExperimentID: accession,
			Accession:    r.accession,
			Layout:       r.layout,
			SourceStatus: r.status,
			Declared:     r.counts,
			Artifacts:    r.artifacts,
		})
	}
	if len(fetched.Samples) == 0 {
		if len(issues) == 0 {
			return fetched, &model.NotFoundError{Source: sourceID, Accession: accession}
		}
		return fetched, &model.SourceError{
			Source: sourceID,
			Kind:   model.SourceNoUsableRuns,
			Detail: fmt.Sprintf("every row has a defect (%d issues)", len(issues)),
		}
	}
	return fetched, nil
}

func (s *Source) Resolve(ctx context.Context, sample model.Sample) ([]model.Artifact, error) {
	fetched, err := s.Fetch(ctx, sample.Accession)
	if err != nil {
		return nil, err
	}
	for _, candidate := range fetched.Samples {
		if candidate.Accession == sample.Accession {
			return candidate.Artifacts, nil
		}
	}
	return nil, &model.NotFoundError{Source: sourceID, Accession: sample.Accession}
}

func (s *Source) reportURL(accession string) string {
	query := url.Values{}
	query.Set("accession", accession)
	query.Set("result", "read_run")
	query.Set("fields", strings.Join(requiredColumns, ","))
	query.Set("format", "tsv")
	return s.base + "/filereport?" + query.Encode()
}

func (s *Source) get(ctx context.Context, endpoint secret.Endpoint) (*model.RawResponse, error) {
	resp, err := s.transport.Get(ctx, endpoint, nil)
	if err != nil {
		return nil, &model.SourceError{Source: sourceID, Kind: model.SourceUnavailable, Detail: "the transport failed", Err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, s.maxResponse+1))
	if err != nil {
		return nil, &model.SourceError{Source: sourceID, Kind: model.SourceUnavailable, Detail: "the body could not be read", Err: err}
	}
	if int64(len(body)) > s.maxResponse {
		return nil, &model.SourceError{Source: sourceID, Kind: model.SourceTooLarge, Detail: fmt.Sprintf("the body exceeds %d bytes", s.maxResponse)}
	}

	raw := &model.RawResponse{
		Source:      sourceID,
		URL:         endpoint,
		StatusCode:  resp.StatusCode,
		ContentType: resp.ContentType,
		FetchedAt:   s.now().UTC(),
		Body:        body,
	}
	if resp.StatusCode != 200 {
		return raw, &model.SourceError{Source: sourceID, Kind: model.SourceUnexpectedStatus, Detail: fmt.Sprintf("the status is %d", resp.StatusCode)}
	}
	if strings.Contains(strings.ToLower(resp.ContentType), "html") {
		return raw, &model.SourceError{Source: sourceID, Kind: model.SourceNotTabular, Detail: "the content type is html, expected a table"}
	}
	return raw, nil
}
