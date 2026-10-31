package ena_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/source/ena"
	"github.com/colligo-go/colligo/transport"
)

type fakeTransport struct {
	response *transport.Response
	err      error
	requests []secret.Endpoint
	ranges   []*transport.ByteRange
}

func (f *fakeTransport) Get(_ context.Context, url secret.Endpoint, rng *transport.ByteRange) (*transport.Response, error) {
	f.requests = append(f.requests, url)
	f.ranges = append(f.ranges, rng)
	return f.response, f.err
}

func respond(status int, contentType string, body []byte) *fakeTransport {
	return &fakeTransport{response: &transport.Response{
		Status:      status,
		ContentType: contentType,
		Body:        io.NopCloser(bytes.NewReader(body)),
	}}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s is missing: %v", name, err)
	}
	return body
}

func mutate(t *testing.T, body []byte, old, replacement string) []byte {
	t.Helper()
	if !bytes.Contains(body, []byte(old)) {
		t.Fatalf("mutation target %q is not in the fixture", old)
	}
	return bytes.Replace(body, []byte(old), []byte(replacement), 1)
}

func newSource(f *fakeTransport, opts ...ena.Option) *ena.Source {
	base := []ena.Option{
		ena.WithTransport(f),
		ena.WithClock(func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) }),
	}
	return ena.New(append(base, opts...)...)
}

type wantFile struct {
	name   string
	size   int64
	md5    string
	paired bool
}

type wantRun struct {
	accession string
	layout    string
	reads     int64
	bases     int64
	files     []wantFile
}

var recordedRuns = []wantRun{
	{"DRR021374", "PAIRED", 13767, 3454158, []wantFile{
		{"DRR021374_1.fastq.gz", 730247, "8f8e57d7be5e2ad58b836563fe99457e", true},
		{"DRR021374_2.fastq.gz", 935734, "670182accbd67c947b86a2d4d8b3aef3", true},
	}},
	{"ERR12477684", "PAIRED", 3524, 528600, []wantFile{
		{"ERR12477684_1.fastq.gz", 102067, "02a4a06a9c0e449b4ec1a3a62f98e127", true},
		{"ERR12477684_2.fastq.gz", 123411, "06e65620b3ab8992821b8ca985deb125", true},
	}},
	{"ERR5060520", "SINGLE", 8014, 1862354, []wantFile{
		{"ERR5060520_1.fastq.gz", 179620, "a1838e1e2131672cdc59cbd563608ea2", true},
		{"ERR5060520_2.fastq.gz", 200969, "ac3444e11fd3fa6ebd812aee53428686", true},
	}},
	{"ERR5514981", "SINGLE", 4923, 598798, []wantFile{
		{"ERR5514981.fastq.gz", 684, "d3ec59dbfe77d2611589c04903e317f9", false},
		{"ERR5514981_1.fastq.gz", 90319, "c26407607b230b54ba4111eb1d417d1e", true},
		{"ERR5514981_2.fastq.gz", 93286, "f7cfd9bc4b536670201509faa4eb5737", true},
	}},
	{"SRR10663037", "PAIRED", 12859, 6454967, []wantFile{
		{"SRR10663037.fastq.gz", 252, "ee8e642d6f9844f3cf1ae077b1c63058", false},
		{"SRR10663037_1.fastq.gz", 1317645, "cf50c11c78a5140271a8acd84bae7009", true},
		{"SRR10663037_2.fastq.gz", 1751604, "74d0f13e581440e20a5777dadb46474e", true},
	}},
}

func TestFetchParsesRecordedRuns(t *testing.T) {
	for _, want := range recordedRuns {
		t.Run(want.accession, func(t *testing.T) {
			body := fixture(t, want.accession+".tsv")
			fetched, err := newSource(respond(200, "text/plain", body)).Fetch(context.Background(), want.accession)
			if err != nil {
				t.Fatal(err)
			}
			if len(fetched.Issues) != 0 {
				t.Fatalf("issues: %+v", fetched.Issues)
			}
			if len(fetched.Samples) != 1 {
				t.Fatalf("got %d samples", len(fetched.Samples))
			}
			sample := fetched.Samples[0]
			if sample.Accession != want.accession || sample.Layout != want.layout || sample.SourceStatus != "public" {
				t.Errorf("sample = %+v", sample)
			}
			if !declared(sample.Declared.ReadCount, want.reads) || !declared(sample.Declared.BaseCount, want.bases) {
				t.Errorf("declared = %v %v, want reads %d bases %d", shown(sample.Declared.ReadCount), shown(sample.Declared.BaseCount), want.reads, want.bases)
			}
			if len(sample.Artifacts) != len(want.files) {
				t.Fatalf("got %d artifacts, want %d", len(sample.Artifacts), len(want.files))
			}
			for i, wf := range want.files {
				got := sample.Artifacts[i]
				if got.Name.String() != wf.name || got.Size != wf.size || got.MD5 != wf.md5 {
					t.Errorf("artifact %d = %s %d %s", i, got.Name, got.Size, got.MD5)
				}
				wantPair := ""
				if wf.paired {
					wantPair = want.accession
				}
				if got.PairID != wantPair || got.Format != "fastq" || got.SourceStatus != "public" {
					t.Errorf("artifact %d pair %q format %q status %q", i, got.PairID, got.Format, got.SourceStatus)
				}
			}
		})
	}
}

func TestFetchBuildsHTTPSURLFromFTPPath(t *testing.T) {
	body := fixture(t, "DRR021374.tsv")
	fetched, err := newSource(respond(200, "text/plain", body)).Fetch(context.Background(), "DRR021374")
	if err != nil {
		t.Fatal(err)
	}
	got := fetched.Samples[0].Artifacts[0].URL.String()
	want := "https://ftp.sra.ebi.ac.uk/vol1/fastq/DRR021/DRR021374/DRR021374_1.fastq.gz"
	if got != want {
		t.Errorf("URL = %s, want %s", got, want)
	}
}

func TestFetchKeepsTheRawResponse(t *testing.T) {
	body := fixture(t, "ERR5514981.tsv")
	f := respond(200, "text/plain", body)
	fetched, err := newSource(f).Fetch(context.Background(), "ERR5514981")
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Raw) != 1 {
		t.Fatalf("got %d raw responses", len(fetched.Raw))
	}
	raw := fetched.Raw[0]
	if !bytes.Equal(raw.Body, body) || raw.StatusCode != 200 || raw.ContentType != "text/plain" || raw.Source != "ena" {
		t.Errorf("raw = %+v", raw)
	}
	if !raw.FetchedAt.Equal(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("FetchedAt = %v", raw.FetchedAt)
	}
	if len(f.requests) != 1 || f.ranges[0] != nil {
		t.Fatalf("requests = %v ranges = %v", f.requests, f.ranges)
	}
	query := f.requests[0].String()
	if !strings.Contains(query, "/filereport") || strings.Contains(query, "?") {
		t.Errorf("endpoint %s", query)
	}
}

func TestFetchRunThatDoesNotExist(t *testing.T) {
	body := fixture(t, "ERR0000001-not-found.tsv")
	fetched, err := newSource(respond(200, "text/plain", body)).Fetch(context.Background(), "ERR0000001")
	var notFound *model.NotFoundError
	if !errors.As(err, &notFound) || notFound.Accession != "ERR0000001" {
		t.Fatalf("err = %v", err)
	}
	if len(fetched.Raw) != 1 {
		t.Errorf("raw response was not kept")
	}
}

func sourceErrorKind(t *testing.T, err error) model.SourceErrorKind {
	t.Helper()
	var sourceErr *model.SourceError
	if !errors.As(err, &sourceErr) {
		t.Fatalf("err = %v, want *model.SourceError", err)
	}
	if sourceErr.Severity() != model.Blocking {
		t.Errorf("severity = %s", sourceErr.Severity())
	}
	return sourceErr.Kind
}

func TestFetchRejectsHTMLErrorPage(t *testing.T) {
	page := fixture(t, "html-500.synthetic.html")
	cases := []struct {
		name        string
		status      int
		contentType string
		want        model.SourceErrorKind
	}{
		{"html with status 200", 200, "text/html;charset=utf-8", model.SourceNotTabular},
		{"html declared as text", 200, "text/plain", model.SourceNotTabular},
		{"html with status 500", 500, "text/html", model.SourceUnexpectedStatus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fetched, err := newSource(respond(tc.status, tc.contentType, page)).Fetch(context.Background(), "DRR021374")
			if got := sourceErrorKind(t, err); got != tc.want {
				t.Errorf("kind = %s, want %s", got, tc.want)
			}
			if len(fetched.Samples) != 0 || len(fetched.Raw) != 1 {
				t.Errorf("samples %d raw %d", len(fetched.Samples), len(fetched.Raw))
			}
			if strings.Contains(err.Error(), "Internal Server Error") {
				t.Errorf("the error leaks the body: %v", err)
			}
		})
	}
}

func TestFetchRejectsBadResponses(t *testing.T) {
	good := fixture(t, "DRR021374.tsv")
	cases := []struct {
		name string
		body []byte
		want model.SourceErrorKind
	}{
		{"empty body", nil, model.SourceEmptyResponse},
		{"only whitespace", []byte("\n  \n"), model.SourceEmptyResponse},
		{"plain text message", []byte("Accession(s) BADACC not valid for search requests"), model.SourceNotTabular},
		{"missing column", mutate(t, good, "\tread_count", "\tread_total"), model.SourceMissingColumn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newSource(respond(200, "text/plain", tc.body)).Fetch(context.Background(), "DRR021374")
			if got := sourceErrorKind(t, err); got != tc.want {
				t.Errorf("kind = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestFetchMissingColumnNamesIt(t *testing.T) {
	body := mutate(t, fixture(t, "DRR021374.tsv"), "\tbase_count", "\tbase_total")
	_, err := newSource(respond(200, "text/plain", body)).Fetch(context.Background(), "DRR021374")
	if err == nil || !strings.Contains(err.Error(), "base_count") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchAcceptsReorderedColumns(t *testing.T) {
	body := []byte("status\tbase_count\tread_count\tfastq_bytes\tfastq_md5\tfastq_ftp\tlibrary_layout\trun_accession\textra\n" +
		"public\t100\t5\t10\t8f8e57d7be5e2ad58b836563fe99457e\thost/x/RUN1.fastq.gz\tSINGLE\tRUN1\tignored\n")
	fetched, err := newSource(respond(200, "text/plain", body)).Fetch(context.Background(), "ERR1")
	if err != nil {
		t.Fatal(err)
	}
	if !declared(fetched.Samples[0].Declared.BaseCount, 100) || fetched.Samples[0].Artifacts[0].Name.String() != "RUN1.fastq.gz" {
		t.Errorf("sample = %+v", fetched.Samples[0])
	}
}

func TestFetchRowDefectsBecomeIssues(t *testing.T) {
	good := fixture(t, "DRR021374.tsv")
	const md5a = "8f8e57d7be5e2ad58b836563fe99457e"
	cases := []struct {
		name string
		body []byte
		code string
	}{
		{"empty md5 list", mutate(t, good, md5a+";670182accbd67c947b86a2d4d8b3aef3", ""), "empty_field"},
		{"empty run accession", mutate(t, good, "DRR021374\tPAIRED", "\tPAIRED"), "empty_field"},
		{"one file column empty", mutate(t, good, "730247;935734", ""), "empty_field"},
		{"read count not a number", mutate(t, good, "\t13767\t", "\tmany\t"), "bad_number"},
		{"md5 list shorter than files", mutate(t, good, md5a+";670182accbd67c947b86a2d4d8b3aef3", md5a), "misaligned_lists"},
		{"bytes list longer than files", mutate(t, good, "730247;935734", "730247;935734;1"), "misaligned_lists"},
		{"url list shorter than md5", mutate(t, good, ";ftp.sra.ebi.ac.uk/vol1/fastq/DRR021/DRR021374/DRR021374_2.fastq.gz", ""), "misaligned_lists"},
		{"md5 not hex", mutate(t, good, md5a, "zzze57d7be5e2ad58b836563fe99457e"), "bad_md5"},
		{"md5 too short", mutate(t, good, md5a, "8f8e57d7"), "bad_md5"},
		{"bytes not a number", mutate(t, good, "730247", "73O247"), "bad_number"},
		{"negative bytes", mutate(t, good, "730247", "-5"), "bad_number"},
		{"base count not a number", mutate(t, good, "3454158", "many"), "bad_number"},
		{"empty entry in url list", mutate(t, good, "DRR021374_1.fastq.gz;", "DRR021374_1.fastq.gz; ;"), "misaligned_lists"},
		{"ragged row", mutate(t, good, "\tpublic", "\tpublic\textra"), "ragged_row"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fetched, err := newSource(respond(200, "text/plain", tc.body)).Fetch(context.Background(), "DRR021374")
			if got := sourceErrorKind(t, err); got != model.SourceNoUsableRuns {
				t.Errorf("kind = %s", got)
			}
			if len(fetched.Samples) != 0 {
				t.Errorf("a defective run produced a sample")
			}
			if len(fetched.Issues) == 0 || fetched.Issues[0].Code != tc.code {
				t.Fatalf("issues = %+v, want code %s", fetched.Issues, tc.code)
			}
			issue := fetched.Issues[0]
			if issue.Severity != model.Blocking || issue.Subject == "" || issue.Message == "" {
				t.Errorf("issue = %+v", issue)
			}
		})
	}
}

func declared(got *int64, want int64) bool {
	return got != nil && *got == want
}

func shown(count *int64) string {
	if count == nil {
		return "not declared"
	}
	return strconv.FormatInt(*count, 10)
}

func TestFetchKeepsARunWhoseDescriptionIsEmpty(t *testing.T) {
	good := fixture(t, "DRR021374.tsv")
	cases := []struct {
		name   string
		body   []byte
		column string
		check  func(t *testing.T, s model.Sample)
	}{
		{"read count", mutate(t, good, "\t13767\t", "\t\t"), "read_count", func(t *testing.T, s model.Sample) {
			if s.Declared.ReadCount != nil || !declared(s.Declared.BaseCount, 3454158) {
				t.Errorf("declared = %v %v, want no reads and the bases", shown(s.Declared.ReadCount), shown(s.Declared.BaseCount))
			}
		}},
		{"base count", mutate(t, good, "\t3454158\t", "\t\t"), "base_count", func(t *testing.T, s model.Sample) {
			if s.Declared.BaseCount != nil || !declared(s.Declared.ReadCount, 13767) {
				t.Errorf("declared = %v %v, want the reads and no bases", shown(s.Declared.ReadCount), shown(s.Declared.BaseCount))
			}
		}},
		{"status", mutate(t, good, "\tpublic", "\t"), "status", func(t *testing.T, s model.Sample) {
			if s.SourceStatus != "" || len(s.Artifacts) != 2 {
				t.Errorf("status %q, %d artifacts", s.SourceStatus, len(s.Artifacts))
			}
		}},
		{"layout", mutate(t, good, "\tPAIRED\t", "\t\t"), "library_layout", func(t *testing.T, s model.Sample) {
			if s.Layout != "" || len(s.Artifacts) != 2 {
				t.Errorf("layout %q, %d artifacts", s.Layout, len(s.Artifacts))
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fetched, err := newSource(respond(200, "text/plain", tc.body)).Fetch(context.Background(), "DRR021374")
			if err != nil {
				t.Fatal(err)
			}
			if len(fetched.Samples) != 1 {
				t.Fatalf("got %d samples, the run must be kept", len(fetched.Samples))
			}
			tc.check(t, fetched.Samples[0])
			if len(fetched.Issues) != 1 {
				t.Fatalf("issues = %+v", fetched.Issues)
			}
			issue := fetched.Issues[0]
			if issue.Severity != model.Advisory || issue.Code != "empty_field" || issue.Subject != "DRR021374" || !strings.Contains(issue.Message, tc.column) {
				t.Errorf("issue = %+v", issue)
			}
		})
	}
}

func TestFetchKeepsARunWithoutFilesAndItsStatus(t *testing.T) {
	good := fixture(t, "DRR021374.tsv")
	row := "DRR021374\tPAIRED\t\t\t\t13767\t3454158\tsuppressed\n"
	body := append(good[:bytes.IndexByte(good, '\n')+1:bytes.IndexByte(good, '\n')+1], row...)
	fetched, err := newSource(respond(200, "text/plain", body)).Fetch(context.Background(), "DRR021374")
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Issues) != 0 || len(fetched.Samples) != 1 {
		t.Fatalf("samples %d, issues %+v", len(fetched.Samples), fetched.Issues)
	}
	sample := fetched.Samples[0]
	if sample.SourceStatus != "suppressed" || len(sample.Artifacts) != 0 || !declared(sample.Declared.BaseCount, 3454158) {
		t.Errorf("sample = %+v", sample)
	}
}

func TestFetchDefectiveRunDoesNotHideTheOthers(t *testing.T) {
	good := fixture(t, "DRR021374.tsv")
	other := fixture(t, "ERR12477684.tsv")
	otherRow := other[bytes.IndexByte(other, '\n')+1:]
	broken := mutate(t, otherRow, "02a4a06a9c0e449b4ec1a3a62f98e127", "nothex")
	body := append(append([]byte{}, good...), broken...)

	fetched, err := newSource(respond(200, "text/plain", body)).Fetch(context.Background(), "PRJEB1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Samples) != 1 || fetched.Samples[0].Accession != "DRR021374" {
		t.Fatalf("samples = %+v", fetched.Samples)
	}
	if len(fetched.Issues) != 1 || fetched.Issues[0].Subject != "ERR12477684" || fetched.Issues[0].Code != "bad_md5" {
		t.Fatalf("issues = %+v", fetched.Issues)
	}
}

func TestFetchSeveralRunsFromOneStudy(t *testing.T) {
	var body []byte
	for i, want := range recordedRuns {
		raw := fixture(t, want.accession+".tsv")
		if i == 0 {
			body = append(body, raw...)
			continue
		}
		body = append(body, raw[bytes.IndexByte(raw, '\n')+1:]...)
	}
	fetched, err := newSource(respond(200, "text/plain", body)).Fetch(context.Background(), "PRJEB1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Samples) != len(recordedRuns) || len(fetched.Issues) != 0 {
		t.Fatalf("samples %d issues %+v", len(fetched.Samples), fetched.Issues)
	}
	for i, want := range recordedRuns {
		if fetched.Samples[i].Accession != want.accession || fetched.Samples[i].ExperimentID != "PRJEB1" {
			t.Errorf("sample %d = %s in %s", i, fetched.Samples[i].Accession, fetched.Samples[i].ExperimentID)
		}
	}
	if fetched.Experiment.ID != "PRJEB1" || fetched.Experiment.Source != "ena" {
		t.Errorf("experiment = %+v", fetched.Experiment)
	}
}

func TestFetchDuplicateRunIsReported(t *testing.T) {
	good := fixture(t, "DRR021374.tsv")
	row := good[bytes.IndexByte(good, '\n')+1:]
	body := append(append([]byte{}, good...), row...)
	fetched, err := newSource(respond(200, "text/plain", body)).Fetch(context.Background(), "DRR021374")
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Samples) != 1 || len(fetched.Issues) != 1 || fetched.Issues[0].Code != "duplicate_run" {
		t.Fatalf("samples %d issues %+v", len(fetched.Samples), fetched.Issues)
	}
}

func TestFetchToleratesWindowsLineEndings(t *testing.T) {
	body := bytes.ReplaceAll(fixture(t, "DRR021374.tsv"), []byte("\n"), []byte("\r\n"))
	fetched, err := newSource(respond(200, "text/plain", body)).Fetch(context.Background(), "DRR021374")
	if err != nil || len(fetched.Issues) != 0 || fetched.Samples[0].SourceStatus != "public" {
		t.Fatalf("err %v issues %+v", err, fetched.Issues)
	}
}

func TestFetchRefusesAResponseAboveTheLimit(t *testing.T) {
	body := fixture(t, "DRR021374.tsv")
	source := newSource(respond(200, "text/plain", body), ena.WithMaxResponseBytes(int64(len(body)-1)))
	_, err := source.Fetch(context.Background(), "DRR021374")
	if got := sourceErrorKind(t, err); got != model.SourceTooLarge {
		t.Errorf("kind = %s", got)
	}
	source = newSource(respond(200, "text/plain", body), ena.WithMaxResponseBytes(int64(len(body))))
	if _, err := source.Fetch(context.Background(), "DRR021374"); err != nil {
		t.Errorf("a response exactly at the limit failed: %v", err)
	}
}

func TestFetchWrapsTransportFailure(t *testing.T) {
	cause := errors.New("connection reset")
	_, err := newSource(&fakeTransport{err: cause}).Fetch(context.Background(), "DRR021374")
	if got := sourceErrorKind(t, err); got != model.SourceUnavailable {
		t.Errorf("kind = %s", got)
	}
	if !errors.Is(err, cause) {
		t.Errorf("the cause is not reachable with errors.Is")
	}
}

func TestFetchWithoutTransport(t *testing.T) {
	_, err := ena.New().Fetch(context.Background(), "DRR021374")
	if got := sourceErrorKind(t, err); got != model.SourceNoTransport {
		t.Errorf("kind = %s", got)
	}
}

func TestFetchRejectsHostileAccessions(t *testing.T) {
	for _, accession := range []string{"", "drr021374", "DRR021374&fields=all", "DRR0213 74", "../etc", "DRR021374\n", "ERR1;ERR2"} {
		f := respond(200, "text/plain", nil)
		_, err := newSource(f).Fetch(context.Background(), accession)
		if got := sourceErrorKind(t, err); got != model.SourceInvalidAccession {
			t.Errorf("%q: kind = %s", accession, got)
		}
		if len(f.requests) != 0 {
			t.Errorf("%q reached the transport", accession)
		}
	}
}

func TestResolveReturnsTheArtifactsOfTheRun(t *testing.T) {
	body := fixture(t, "SRR10663037.tsv")
	artifacts, err := newSource(respond(200, "text/plain", body)).Resolve(context.Background(), model.Sample{Accession: "SRR10663037"})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 3 || artifacts[0].Name.String() != "SRR10663037.fastq.gz" {
		t.Errorf("artifacts = %+v", artifacts)
	}
}

func TestResolveRunThatDoesNotExist(t *testing.T) {
	body := fixture(t, "ERR0000001-not-found.tsv")
	_, err := newSource(respond(200, "text/plain", body)).Resolve(context.Background(), model.Sample{Accession: "ERR0000001"})
	var notFound *model.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestSourceID(t *testing.T) {
	if ena.New().ID() != "ena" {
		t.Errorf("ID = %s", ena.New().ID())
	}
}

func TestBaseURLOption(t *testing.T) {
	base, err := secret.NewEndpoint("https://mirror.example/api/")
	if err != nil {
		t.Fatal(err)
	}
	f := respond(200, "text/plain", fixture(t, "DRR021374.tsv"))
	if _, err := newSource(f, ena.WithBaseURL(base)).Fetch(context.Background(), "DRR021374"); err != nil {
		t.Fatal(err)
	}
	if got := f.requests[0].String(); got != "https://mirror.example/api/filereport" {
		t.Errorf("endpoint = %s", got)
	}
}
