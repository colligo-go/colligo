package verifier

import (
	"context"
	"strings"
	"time"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/request"
)

// Fetch obtains what a source declares for the accession of a request. It is
// the only place the network is reached, and Check calls it only when the
// request carries a source.
type Fetch func(ctx context.Context, ref request.SourceRef) (Declaration, error)

// FetchError is a failure of the source, as opposed to a failure of the
// files: nothing was compared, so nothing is verified.
type FetchError struct {
	Ref request.SourceRef
	Err error
}

func (e *FetchError) Error() string { return e.Err.Error() }
func (e *FetchError) Unwrap() error { return e.Err }

// Check runs the checks a request asks for, whichever form of input produced
// it: a samplesheet declares which files are mates, a source reference is
// compared after it is fetched, and anything else is a plain check. The
// source is queried once, after the files are resolved and before any is read
// in full.
func Check(ctx context.Context, req request.Request, fetch Fetch, opts Options) (Report, error) {
	started := time.Now()
	var (
		report Report
		err    error
	)
	switch {
	case req.Sheet != nil:
		report, err = CheckSamplesheet(ctx, *req.Sheet, opts)
	case req.Source != nil:
		var decl Declaration
		if decl, err = fetch(ctx, *req.Source); err != nil {
			return Report{}, &FetchError{Ref: *req.Source, Err: err}
		}
		decl.From = string(req.Source.Provenance)
		report, err = CheckAgainstSource(ctx, req.Names(), decl, opts)
		report.Elapsed = time.Since(started)
	default:
		report, err = CheckAll(ctx, req.Names(), opts)
	}
	if err != nil {
		return Report{}, err
	}
	annotateListLines(report, req)
	report.Skipped, report.SkippedLinks = req.Skipped, req.SkippedLinks
	return report, nil
}

func annotateListLines(report Report, req request.Request) {
	if req.Sheet != nil || len(report.Files) != len(req.Files) {
		return
	}
	for i, file := range req.Files {
		lines := file.Lines(request.FromListLine)
		r := report.Files[i]
		if len(lines) == 0 || r.Status != StatusUnreadable {
			continue
		}
		where := "list " + request.DescribeLines(lines)
		for j := range r.Assertions {
			a := &r.Assertions[j]
			if a.ID == diagnoses.ReadFailed && !a.Passed {
				a.Declared = where
				a.Observation = strings.TrimSuffix(a.Observation, ".") + " (listed in " + where + ")."
			}
		}
	}
}
