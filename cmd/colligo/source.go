package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/source"
	"github.com/colligo-go/colligo/source/ena"
	"github.com/colligo-go/colligo/transport"
	"github.com/colligo-go/colligo/verifier"
)

type sourceRef struct {
	id        string
	accession string
}

func parseSourceRef(value string) (sourceRef, error) {
	id, accession, found := strings.Cut(value, ":")
	if !found || accession == "" {
		return sourceRef{}, fmt.Errorf("--source takes <source>:<accession>, for example ena:SRR10663037")
	}
	if id != "ena" {
		return sourceRef{}, fmt.Errorf("unknown source %q; the only source today is ena", id)
	}
	return sourceRef{id: id, accession: accession}, nil
}

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

func fetchDeclaration(ctx context.Context, ref sourceRef, net transport.Transport) (verifier.Declaration, error) {
	adapter, err := adapterFor(net)
	if err != nil {
		return verifier.Declaration{}, err
	}
	fetched, err := adapter.Fetch(ctx, ref.accession)
	if err != nil {
		return verifier.Declaration{}, err
	}
	return verifier.Declaration{Source: ref.id, Accession: ref.accession, Samples: fetched.Samples, Issues: fetched.Issues}, nil
}

func sourceFailed(err error, ref sourceRef, stderr io.Writer) int {
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "colligo: interrupted; nothing was saved and nothing is half-done")
		return verifier.ExitInterrupted
	}
	var bad *model.SourceError
	if errors.As(err, &bad) && bad.Kind == model.SourceInvalidAccession {
		fmt.Fprintf(stderr, "colligo check --source: %q is not an %s accession\n", ref.accession, ref.id)
		return verifier.ExitUsage
	}
	fmt.Fprintf(stderr, "colligo check --source: %v\nnothing was compared with the source and nothing is verified\n", err)
	return verifier.ExitFatal
}
