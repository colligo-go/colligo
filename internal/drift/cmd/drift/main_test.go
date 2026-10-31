package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/transport"
)

const fixtures = "../../../../source/ena/testdata"

type live struct {
	mutate func(accession string, body []byte) []byte
	err    error
}

func (l live) Get(_ context.Context, endpoint secret.Endpoint, _ *transport.ByteRange) (*transport.Response, error) {
	if l.err != nil {
		return nil, l.err
	}
	target, err := url.Parse(endpoint.Reveal())
	if err != nil {
		return nil, err
	}
	accession := target.Query().Get("accession")
	body, err := os.ReadFile(filepath.Join(fixtures, accession+".tsv"))
	if err != nil {
		body, err = os.ReadFile(filepath.Join(fixtures, "ERR0000001-not-found.tsv"))
		if err != nil {
			return nil, err
		}
	}
	if l.mutate != nil {
		body = l.mutate(accession, body)
	}
	return &transport.Response{Status: 200, ContentType: "text/plain", ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func execute(t *testing.T, dir string, source transport.Transport) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(dir, source, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestUnchangedSourceIsClean(t *testing.T) {
	code, out, _ := execute(t, fixtures, live{})
	if code != exitClean || strings.Contains(out, "DRIFT") {
		t.Errorf("code %d\n%s", code, out)
	}
	if strings.Count(out, "ok ") != 6 {
		t.Errorf("every fixture should be reported:\n%s", out)
	}
}

func TestRenamedColumnIsReportedWithoutValues(t *testing.T) {
	source := live{mutate: func(accession string, body []byte) []byte {
		if accession != "DRR021374" {
			return body
		}
		return bytes.Replace(body, []byte("base_count"), []byte("total_bases"), 1)
	}}
	code, out, _ := execute(t, fixtures, source)
	if code != exitDrift {
		t.Fatalf("code %d\n%s", code, out)
	}
	for _, want := range []string{"DRIFT  DRR021374.tsv", "column disappeared: base_count", "column appeared: total_bases", "1 of 6"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	for _, value := range []string{"3454158", "8f8e57d7be5e2ad58b836563fe99457e", "ftp.sra.ebi.ac.uk"} {
		if strings.Contains(out, value) {
			t.Errorf("a value reached the report: %s", value)
		}
	}
}

func TestCountBecomingTextIsReported(t *testing.T) {
	source := live{mutate: func(accession string, body []byte) []byte {
		if accession != "ERR12477684" {
			return body
		}
		return bytes.Replace(body, []byte("\t3524\t"), []byte("\t3.5e3\t"), 1)
	}}
	code, out, _ := execute(t, fixtures, source)
	if code != exitDrift || !strings.Contains(out, "column read_count: cell classes were [integer], are now [word]") {
		t.Errorf("code %d\n%s", code, out)
	}
}

func TestHTMLInPlaceOfTheTableIsDrift(t *testing.T) {
	source := live{mutate: func(string, []byte) []byte { return []byte("<html><body>maintenance</body></html>\n") }}
	code, out, _ := execute(t, fixtures, source)
	if code != exitDrift || !strings.Contains(out, "table: tabular was true, is now false") {
		t.Errorf("code %d\n%s", code, out)
	}
}

func TestAnOutageIsNotDrift(t *testing.T) {
	code, out, errOut := execute(t, fixtures, live{err: &transport.RetryExhaustedError{Attempts: 4, Err: errors.New("503")}})
	if code != exitFailure || strings.Contains(out, "DRIFT") || !strings.Contains(errOut, "not drift") {
		t.Errorf("code %d\nout %s\nerr %s", code, out, errOut)
	}
}

func TestTamperedFixtureStopsTheJob(t *testing.T) {
	dir := t.TempDir()
	entries, err := os.ReadDir(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(fixtures, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name() == "DRR021374.tsv" {
			data = append(data, '\n', 'x')
		}
		if err := os.WriteFile(filepath.Join(dir, entry.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	code, _, errOut := execute(t, dir, live{})
	if code != exitFailure || !strings.Contains(errOut, "does not match the sha256") {
		t.Errorf("code %d\n%s", code, errOut)
	}
}

func TestEmptyDirectoryStopsTheJob(t *testing.T) {
	code, _, errOut := execute(t, t.TempDir(), live{})
	if code != exitFailure || !strings.Contains(errOut, "no recorded response") {
		t.Errorf("code %d\n%s", code, errOut)
	}
}
