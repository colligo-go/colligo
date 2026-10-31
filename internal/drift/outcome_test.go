package drift

import (
	"context"
	"errors"
	"testing"

	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/source"
	"github.com/colligo-go/colligo/transport"
)

func TestObserveSeparatesNotFoundFromFailure(t *testing.T) {
	notFound := Observe(source.Fetched{}, &model.NotFoundError{Source: "ena", Accession: "X"})
	failure := Observe(source.Fetched{}, &model.SourceError{Kind: model.SourceNotTabular})
	if notFound.Result != "not_found" || failure.Result != "error:not_tabular" {
		t.Errorf("%+v %+v", notFound, failure)
	}
}

func TestObserveDeduplicatesAndSortsIssueCodes(t *testing.T) {
	got := Observe(source.Fetched{Issues: []model.ValidationIssue{{Code: "b"}, {Code: "a"}, {Code: "b"}}}, nil)
	if len(got.Codes) != 2 || got.Codes[0] != "a" || got.Codes[1] != "b" {
		t.Errorf("%v", got.Codes)
	}
}

func TestIsOutageIsNotDrift(t *testing.T) {
	endpoint, err := secret.NewEndpoint("https://example.org/x")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		err    error
		outage bool
	}{
		{"nil", nil, false},
		{"server error", &model.SourceError{Kind: model.SourceUnexpectedStatus}, true},
		{"network", &model.SourceError{Kind: model.SourceUnavailable, Err: errors.New("dial")}, true},
		{"new content type", &model.SourceError{Kind: model.SourceUnavailable, Err: &transport.ContentTypeError{Endpoint: endpoint, Got: "text/html"}}, false},
		{"endpoint now 404", &model.SourceError{Kind: model.SourceUnavailable, Err: &transport.NotFoundError{Endpoint: endpoint, Status: 404}}, false},
		{"parse failure", &model.SourceError{Kind: model.SourceNotTabular}, false},
	}
	for _, tc := range cases {
		if got := IsOutage(tc.err); got != tc.outage {
			t.Errorf("%s: outage %t, want %t", tc.name, got, tc.outage)
		}
	}
}

func TestDriftCauseAppearsInTheOutcome(t *testing.T) {
	endpoint, _ := secret.NewEndpoint("https://example.org/x")
	err := &model.SourceError{Kind: model.SourceUnavailable, Err: &transport.ContentTypeError{Endpoint: endpoint, Got: "text/html"}}
	lines := DiffOutcome(Outcome{Result: "ok"}, Observe(source.Fetched{}, err))
	if len(lines) != 1 || lines[0] != "adapter: result was ok, is now error:unavailable (content type refused)" {
		t.Errorf("%q", lines)
	}
}

func TestDiffOutcomeReportsDefectCodes(t *testing.T) {
	lines := DiffOutcome(Outcome{Result: "ok"}, Outcome{Result: "ok", Codes: []string{"new_defect"}})
	if len(lines) != 1 {
		t.Errorf("%q", lines)
	}
	if lines := DiffOutcome(Outcome{Result: "ok"}, Outcome{Result: "ok"}); len(lines) != 0 {
		t.Errorf("%q", lines)
	}
}

func TestDiffEnvelope(t *testing.T) {
	if lines := DiffEnvelope(model.RawResponse{StatusCode: 200, ContentType: "text/plain;charset=UTF-8"}); len(lines) != 0 {
		t.Errorf("%q", lines)
	}
	if lines := DiffEnvelope(model.RawResponse{StatusCode: 200, ContentType: "text/tab-separated-values"}); len(lines) != 1 {
		t.Errorf("%q", lines)
	}
	if lines := DiffEnvelope(model.RawResponse{StatusCode: 204, ContentType: "text/plain"}); len(lines) != 1 {
		t.Errorf("%q", lines)
	}
}

func TestReplayServesTheRecordedBodyThroughTheAdapter(t *testing.T) {
	resp, err := Replay{Body: []byte("x")}.Get(context.Background(), secret.Endpoint{}, nil)
	if err != nil || resp.Status != 200 || resp.ContentType != "text/plain" {
		t.Errorf("%+v %v", resp, err)
	}
}
