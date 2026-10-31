package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/request"
	"github.com/colligo-go/colligo/source"
	"github.com/colligo-go/colligo/source/ena"
	"github.com/colligo-go/colligo/transport"
	"github.com/colligo-go/colligo/verifier"
)

func adapterFor(net transport.Transport) (source.Source, error) {
	if net == nil {
		client, err := transport.New()
		if err != nil {
			return nil, err
		}
		net = client
	}
	return ena.New(ena.WithTransport(net)), nil
}

func fetcher(net transport.Transport) verifier.Fetch {
	return func(ctx context.Context, ref request.SourceRef) (verifier.Declaration, error) {
		adapter, err := adapterFor(net)
		if err != nil {
			return verifier.Declaration{}, err
		}
		fetched, err := adapter.Fetch(ctx, ref.Accession)
		if err != nil {
			return verifier.Declaration{}, err
		}
		return verifier.Declaration{Source: ref.ID, Accession: ref.Accession, Samples: fetched.Samples, Issues: fetched.Issues}, nil
	}
}

func sourceFailed(failure *verifier.FetchError, stderr io.Writer) int {
	if errors.Is(failure, context.Canceled) {
		fmt.Fprintln(stderr, "colligo: interrupted; nothing was saved and nothing is half-done")
		return verifier.ExitInterrupted
	}
	var bad *model.SourceError
	if errors.As(failure, &bad) && bad.Kind == model.SourceInvalidAccession {
		fmt.Fprintf(stderr, "colligo check --source: %q is not an %s accession\n", failure.Ref.Accession, failure.Ref.ID)
		return verifier.ExitUsage
	}
	fmt.Fprintf(stderr, "colligo check --source: %v\nnothing was compared with the source and nothing is verified\n", failure)
	return verifier.ExitFatal
}
