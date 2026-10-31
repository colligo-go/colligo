package source

import (
	"context"

	"github.com/colligo-go/colligo/model"
)

type Source interface {
	ID() string
	Fetch(ctx context.Context, accession string) (Fetched, error)
	Resolve(ctx context.Context, s model.Sample) ([]model.Artifact, error)
}

// Fetched is what the metadata part of an adapter returns. Raw is filled even
// when Fetch also returns an error, so the response that failed is kept.
type Fetched struct {
	Experiment model.Experiment
	Samples    []model.Sample
	Raw        []model.RawResponse
	Issues     []model.ValidationIssue
}
