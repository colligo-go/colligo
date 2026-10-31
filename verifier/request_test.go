package verifier

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/request"
	"github.com/colligo-go/colligo/secret"
)

func neverFetch(t *testing.T) Fetch {
	return func(context.Context, request.SourceRef) (Declaration, error) {
		t.Error("the source was fetched for a request that names none")
		return Declaration{}, nil
	}
}

func requestOf(t *testing.T, in request.Inputs) request.Request {
	t.Helper()
	req, err := request.Resolve(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestCheckNeverFetchesWhenTheRequestHasNoSource(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "a.fastq", fastqText("S", 5, "ACGT"))
	report, err := Check(context.Background(), requestOf(t, request.Inputs{Paths: []secret.Name{path}}), neverFetch(t), Options{})
	if err != nil || len(report.Files) != 1 || report.Source != nil {
		t.Fatalf("report %+v err %v", report, err)
	}
}

func TestCheckFetchesTheSourceOnceAndRecordsHowTheAccessionWasObtained(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "ERR1.fastq", fastqText("S", 5, "ACGT"))
	var asked []request.SourceRef
	fetch := func(_ context.Context, ref request.SourceRef) (Declaration, error) {
		asked = append(asked, ref)
		return Declaration{Source: ref.ID, Accession: ref.Accession, Samples: []model.Sample{sample("ERR1", "SINGLE", "public", nil, nil, "ERR1.fastq.gz")}}, nil
	}
	req := requestOf(t, request.Inputs{Paths: []secret.Name{path}, Source: "ena"})
	report, err := Check(context.Background(), req, fetch, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0].Accession != "ERR1" {
		t.Errorf("source asked %+v", asked)
	}
	if report.Source == nil || report.Source.AccessionFrom != "deduced" {
		t.Errorf("source report %+v: the way the accession was obtained is lost", report.Source)
	}
}

func TestCheckReportsASourceFailureAsFetchErrorAndNoReport(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "a.fastq", fastqText("S", 5, "ACGT"))
	boom := errors.New("network down")
	fetch := func(context.Context, request.SourceRef) (Declaration, error) { return Declaration{}, boom }
	_, err := Check(context.Background(), requestOf(t, request.Inputs{Paths: []secret.Name{path}, Source: "ena:ERR1"}), fetch, Options{})
	var failure *FetchError
	if !errors.As(err, &failure) || !errors.Is(err, boom) || failure.Ref.Accession != "ERR1" {
		t.Fatalf("err %v", err)
	}
}

func TestCheckNamesTheListLinesOfAnUnreadableFileAndStillChecksTheRest(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "here.fastq", fastqText("S", 5, "ACGT"))
	list := write(t, dir, "files.txt", []byte("here.fastq\ngone.fastq\n# note\n./gone.fastq\n"))
	report, err := Check(context.Background(), requestOf(t, request.Inputs{ListFile: &list}), neverFetch(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Files) != 2 || report.Files[0].Status != StatusVerified {
		t.Fatalf("the file that exists was not checked: %+v", report.Files)
	}
	gone := report.Files[1]
	if gone.Status != StatusUnreadable {
		t.Fatalf("status %s", gone.Status)
	}
	for _, a := range gone.Assertions {
		if a.ID == diagnoses.ReadFailed && !a.Passed {
			if a.Declared != "list lines 2 and 4" || !strings.Contains(a.Observation, "listed in list lines 2 and 4") {
				t.Errorf("assertion %+v does not name the lines", a)
			}
			return
		}
	}
	t.Error("no read_failed assertion")
}

func TestCheckDoesNotAnnotateAFileGivenAsAnArgument(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "locked.fastq", fastqText("S", 5, "ACGT"))
	req := requestOf(t, request.Inputs{Paths: []secret.Name{path}})
	if err := os.Remove(filepath.Join(dir, "locked.fastq")); err != nil {
		t.Fatal(err)
	}
	report, err := Check(context.Background(), req, neverFetch(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range report.Files[0].Assertions {
		if strings.Contains(a.Observation, "list line") {
			t.Errorf("an argument was described as a list line: %q", a.Observation)
		}
	}
}
