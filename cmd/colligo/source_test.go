package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/internal/exttool"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/transport"
	"github.com/colligo-go/colligo/verifier"
)

type fakeTransport struct {
	body        []byte
	contentType string
	err         error
	calls       int
}

func (f *fakeTransport) Get(_ context.Context, _ secret.Endpoint, _ *transport.ByteRange) (*transport.Response, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	contentType := f.contentType
	if contentType == "" {
		contentType = "text/plain"
	}
	return &transport.Response{Status: 200, ContentType: contentType, ContentLength: int64(len(f.body)), Body: io.NopCloser(bytes.NewReader(f.body))}, nil
}

func recorded(t *testing.T, accession string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "source", "ena", "testdata", accession+".tsv"))
	if err != nil {
		t.Fatalf("recorded ENA response missing: %v", err)
	}
	return body
}

func setField(t *testing.T, body []byte, column string, value func(old string) string) []byte {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	header := strings.Split(lines[0], "\t")
	at := -1
	for i, h := range header {
		if h == column {
			at = i
		}
	}
	if at < 0 || len(lines) < 2 {
		t.Fatalf("no %s column or no row", column)
	}
	for i := 1; i < len(lines); i++ {
		cells := strings.Split(lines[i], "\t")
		cells[at] = value(cells[at])
		lines[i] = strings.Join(cells, "\t")
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func to(value string) func(string) string { return func(string) string { return value } }

type runFile struct {
	name  string
	reads int
}

type runSpec struct {
	accession string
	bases     int64
	files     []runFile
}

var m15Runs = []runSpec{
	{"DRR021374", 3454158, []runFile{{"DRR021374_1.fastq.gz", 13767}, {"DRR021374_2.fastq.gz", 13767}}},
	{"ERR12477684", 528600, []runFile{{"ERR12477684_1.fastq.gz", 1762}, {"ERR12477684_2.fastq.gz", 1762}}},
	{"ERR5060520", 1862354, []runFile{{"ERR5060520_1.fastq.gz", 4007}, {"ERR5060520_2.fastq.gz", 4007}}},
	{"ERR5514981", 598798, []runFile{{"ERR5514981.fastq.gz", 7}, {"ERR5514981_1.fastq.gz", 2458}, {"ERR5514981_2.fastq.gz", 2458}}},
	{"SRR10663037", 6454967, []runFile{{"SRR10663037.fastq.gz", 1}, {"SRR10663037_1.fastq.gz", 12858}, {"SRR10663037_2.fastq.gz", 12858}}},
}

func specOf(t *testing.T, accession string) runSpec {
	t.Helper()
	for _, s := range m15Runs {
		if s.accession == accession {
			return s
		}
	}
	t.Fatalf("no spec for %s", accession)
	return runSpec{}
}

func syntheticRun(spec runSpec) map[string][]byte {
	total := 0
	for _, f := range spec.files {
		total += f.reads
	}
	length := spec.bases / int64(total)
	longer := spec.bases % int64(total)
	out := map[string][]byte{}
	global := int64(0)
	for _, f := range spec.files {
		var b bytes.Buffer
		w := bufio.NewWriter(&b)
		for i := 0; i < f.reads; i++ {
			n := length
			if global < longer {
				n++
			}
			global++
			fmt.Fprintf(w, "@%s.%d\n%s\n+\n%s\n", spec.accession, i+1, strings.Repeat("C", int(n)), strings.Repeat("F", int(n)))
		}
		w.Flush()
		out[f.name] = b.Bytes()
	}
	return out
}

func shortenReads(data []byte) []byte {
	lines := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
	for i := range lines {
		if i%4 == 1 || i%4 == 3 {
			lines[i] = lines[i][:1]
		}
	}
	return append(bytes.Join(lines, []byte("\n")), '\n')
}

func writeDecompressed(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		put(t, dir, strings.TrimSuffix(name, ".gz"), data)
	}
}

func checkSource(t *testing.T, net transport.Transport, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runCheck(args, &out, &errb, false, net)
	return code, out.String(), errb.String()
}

type sourceDoc struct {
	SchemaVersion int `json:"schema_version"`
	ExitCode      int `json:"exit_code"`
	Files         []struct {
		Path       string `json:"path"`
		Status     string `json:"status"`
		Assertions []struct {
			ID     string `json:"id"`
			Passed bool   `json:"passed"`
		} `json:"assertions"`
	} `json:"files"`
	Source struct {
		ID        string `json:"id"`
		Accession string `json:"accession"`
		Runs      []struct {
			Accession  string `json:"accession"`
			Status     string `json:"status"`
			Assertions []struct {
				ID        string `json:"id"`
				Severity  string `json:"severity"`
				Passed    bool   `json:"passed"`
				Skipped   bool   `json:"skipped"`
				SkippedBy string `json:"skipped_by"`
				Declared  string `json:"declared"`
				Observed  string `json:"observed"`
			} `json:"assertions"`
		} `json:"runs"`
	} `json:"source"`
}

func decodeSource(t *testing.T, out string) sourceDoc {
	t.Helper()
	var doc sourceDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return doc
}

type outcome struct {
	passed, skipped bool
	skippedBy       string
	observed        string
	severity        string
}

func runOutcome(t *testing.T, doc sourceDoc, id string) (outcome, bool) {
	t.Helper()
	if len(doc.Source.Runs) != 1 {
		t.Fatalf("want one run, got %d", len(doc.Source.Runs))
	}
	for _, a := range doc.Source.Runs[0].Assertions {
		if a.ID == id {
			return outcome{a.Passed, a.Skipped, a.SkippedBy, a.Observed, a.Severity}, true
		}
	}
	return outcome{}, false
}

func TestSourceRecordedRunsPassWithTheirReading(t *testing.T) {
	readings := map[string]string{
		"DRR021374":   "13767 in each file",
		"ERR12477684": "3524, the sum of the files",
		"ERR5060520":  "8014, the sum of the files",
		"ERR5514981":  "4923, the sum of the files",
		"SRR10663037": "12859, the pairs plus the unpaired files",
	}
	for _, spec := range m15Runs {
		t.Run(spec.accession, func(t *testing.T) {
			dir := t.TempDir()
			writeDecompressed(t, dir, syntheticRun(spec))
			code, out, errText := checkSource(t, &fakeTransport{body: recorded(t, spec.accession)}, "--source", "ena:"+spec.accession, "--json", dir)
			if code != verifier.ExitOK {
				t.Fatalf("exit %d\n%s\n%s", code, out, errText)
			}
			doc := decodeSource(t, out)
			if doc.SchemaVersion != 2 || doc.Source.ID != "ena" || doc.Source.Runs[0].Accession != spec.accession || len(doc.Files) != len(spec.files) {
				t.Fatalf("doc %+v", doc)
			}
			bases, _ := runOutcome(t, doc, "count_mismatch")
			if !bases.passed || bases.observed != fmt.Sprint(spec.bases) {
				t.Fatalf("base_count %+v", bases)
			}
			reads, _ := runOutcome(t, doc, "read_count_unexplained")
			if !reads.passed || reads.observed != readings[spec.accession] {
				t.Fatalf("read_count %+v", reads)
			}
			layout, judged := runOutcome(t, doc, "layout_mismatch")
			if spec.accession == "ERR5060520" || spec.accession == "ERR5514981" {
				if !judged || layout.passed || layout.severity != "advisory" {
					t.Fatalf("%s is labelled SINGLE but its _1 and _2 files are a pair: %+v", spec.accession, layout)
				}
			} else if judged && !layout.passed {
				t.Fatalf("layout %+v", layout)
			}
		})
	}
}

func TestSourceQCFailShortenedRead2IsExit3(t *testing.T) {
	spec := specOf(t, "DRR021374")
	files := syntheticRun(spec)
	files["DRR021374_2.fastq.gz"] = shortenReads(files["DRR021374_2.fastq.gz"])
	dir := t.TempDir()
	writeDecompressed(t, dir, files)
	code, out, _ := checkSource(t, &fakeTransport{body: recorded(t, "DRR021374")}, "--source", "ena:DRR021374", dir)
	if code != verifier.ExitIncomplete {
		t.Fatalf("exit %d, want 3\n%s", code, out)
	}
	for _, want := range []string{"[BLOCKING] count_mismatch (M15)", "but 3454158 were expected", "bases over the 2 file(s) of run DRR021374"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	if got := strings.Count(out, "invalid: fastq"); got != 2 {
		t.Errorf("both files of the run must read invalid, %d do:\n%s", got, out)
	}
	if strings.Contains(out, "pair_count") || strings.Contains(out, "structure_invalid") {
		t.Errorf("the shortened file is structurally valid and still pairs; only the bases may catch it:\n%s", out)
	}
}

func TestSourceExactNamesAreHeldToThePublishedChecksumAndSize(t *testing.T) {
	spec := specOf(t, "ERR12477684")
	dir := t.TempDir()
	body := recorded(t, spec.accession)
	var sums, sizes []string
	for _, f := range spec.files {
		data := gz(syntheticRun(spec)[f.name])
		put(t, dir, f.name, data)
		sum := md5.Sum(data)
		sums, sizes = append(sums, hex.EncodeToString(sum[:])), append(sizes, fmt.Sprint(len(data)))
	}
	matching := setField(t, setField(t, body, "fastq_md5", to(strings.Join(sums, ";"))), "fastq_bytes", to(strings.Join(sizes, ";")))
	code, out, _ := checkSource(t, &fakeTransport{body: matching}, "--source", "ena:"+spec.accession, "--json", dir)
	if code != verifier.ExitOK {
		t.Fatalf("exit %d\n%s", code, out)
	}
	doc := decodeSource(t, out)
	for _, f := range doc.Files {
		checked := map[string]bool{}
		for _, a := range f.Assertions {
			if a.ID == "checksum_mismatch" || a.ID == "size_mismatch" {
				checked[a.ID] = a.Passed
			}
		}
		if !checked["checksum_mismatch"] || !checked["size_mismatch"] {
			t.Fatalf("%s was not held to the published MD5 and size: %+v", f.Path, f.Assertions)
		}
	}

	code, out, _ = checkSource(t, &fakeTransport{body: body}, "--source", "ena:"+spec.accession, dir)
	if code != verifier.ExitFatal || !strings.Contains(out, "checksum_mismatch") {
		t.Fatalf("the recorded MD5 is not the one of the synthetic file: exit %d\n%s", code, out)
	}
}

func TestSourceEmptyReadCountIsNotDeclared(t *testing.T) {
	spec := specOf(t, "DRR021374")
	body := setField(t, recorded(t, spec.accession), "read_count", to(""))
	dir := t.TempDir()
	files := syntheticRun(spec)
	writeDecompressed(t, dir, files)
	code, out, _ := checkSource(t, &fakeTransport{body: body}, "--source", "ena:"+spec.accession, "--json", dir)
	if code != verifier.ExitOK {
		t.Fatalf("an empty read_count must not raise an alarm: exit %d\n%s", code, out)
	}
	doc := decodeSource(t, out)
	reads, _ := runOutcome(t, doc, "read_count_unexplained")
	bases, _ := runOutcome(t, doc, "count_mismatch")
	if !reads.skipped || reads.skippedBy != verifier.NotDeclared || !bases.passed {
		t.Fatalf("read_count %+v, base_count %+v", reads, bases)
	}

	files["DRR021374_2.fastq.gz"] = bytes.Replace(files["DRR021374_2.fastq.gz"], []byte("CCCC"), []byte("C"), 1)
	files["DRR021374_2.fastq.gz"] = bytes.Replace(files["DRR021374_2.fastq.gz"], []byte("FFFF"), []byte("F"), 1)
	writeDecompressed(t, dir, files)
	code, out, _ = checkSource(t, &fakeTransport{body: body}, "--source", "ena:"+spec.accession, dir)
	if code != verifier.ExitIncomplete || !strings.Contains(out, "count_mismatch") || !strings.Contains(out, "read_count_unexplained not evaluated") {
		t.Fatalf("base_count still decides: exit %d\n%s", code, out)
	}
}

func TestSourceSuppressedRunWithoutFilesIsReported(t *testing.T) {
	body := recorded(t, "DRR021374")
	for _, column := range []string{"fastq_ftp", "fastq_md5", "fastq_bytes"} {
		body = setField(t, body, column, to(""))
	}
	body = setField(t, body, "status", to("suppressed"))
	dir := t.TempDir()
	code, out, errText := checkSource(t, &fakeTransport{body: body}, "--source", "ena:DRR021374", dir)
	if code != verifier.ExitIncomplete {
		t.Fatalf("exit %d\n%s\n%s", code, out, errText)
	}
	for _, want := range []string{"run DRR021374 (PAIRED, suppressed)", "[BLOCKING] source_status (M16)", "marks run DRR021374 as suppressed"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestSourceMissingFileAndStrayFile(t *testing.T) {
	spec := specOf(t, "DRR021374")
	dir := t.TempDir()
	files := syntheticRun(spec)
	put(t, dir, "DRR021374_1.fastq", files["DRR021374_1.fastq.gz"])
	put(t, dir, "notes_from_lab.fastq", fastq("LAB", 3))
	code, out, _ := checkSource(t, &fakeTransport{body: recorded(t, spec.accession)}, "--source", "ena:"+spec.accession, dir)
	if code != verifier.ExitIncomplete {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, want := range []string{"source_file_missing", "lists DRR021374_2.fastq.gz", "source_file_unlisted", "notes_from_lab.fastq", "1 of 2 listed file(s) present"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestSourceFailureIsFatalAndNeverVerified(t *testing.T) {
	leaky, err := secret.NewEndpoint("https://www.ebi.ac.uk/ena/portal/api/filereport?accession=DRR021374&token=hunter2")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeDecompressed(t, dir, syntheticRun(specOf(t, "DRR021374")))
	for label, net := range map[string]*fakeTransport{
		"network":   {err: &transport.RetryExhaustedError{Endpoint: leaky, Attempts: 4, Err: errors.New("connection refused")}},
		"html page": {body: []byte("<html><body>500</body></html>"), contentType: "text/html"},
		"not found": {body: recorded(t, "ERR0000001-not-found")},
		"auth":      {err: &transport.AuthError{Endpoint: leaky, Status: 403}},
	} {
		t.Run(label, func(t *testing.T) {
			code, out, errText := checkSource(t, net, "--source", "ena:DRR021374", "--json", dir)
			if code != verifier.ExitFatal {
				t.Fatalf("exit %d\n%s\n%s", code, out, errText)
			}
			if out != "" {
				t.Fatalf("a source failure must not produce a report:\n%s", out)
			}
			if !strings.Contains(errText, "nothing is verified") {
				t.Fatalf("stderr:\n%s", errText)
			}
			for _, leak := range []string{"hunter2", "token=", "?accession"} {
				if strings.Contains(errText, leak) {
					t.Fatalf("stderr shows the query string (%q):\n%s", leak, errText)
				}
			}
		})
	}
}

func TestSourceUsageErrors(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"--source", "DRR021374", dir},
		{"--source", "ena:", dir},
		{"--source", "geo:GSE1", dir},
		{"--source", "ena:not an accession", dir},
		{"--source", "ena:DRR021374", "--quick", dir},
		{"--source", "ena:DRR021374", "--expected", "reads=1", dir},
		{"--source", "ena:DRR021374", "--samplesheet", "s.csv"},
	} {
		net := &fakeTransport{body: recorded(t, "DRR021374")}
		code, _, errText := checkSource(t, net, args...)
		if code != verifier.ExitUsage || errText == "" {
			t.Errorf("%v: exit %d, want 1", args, code)
		}
		if net.calls > 0 && !strings.Contains(args[1], "not an accession") {
			t.Errorf("%v: the source was queried before the usage error", args)
		}
	}
}

func TestValidateHasNoSourceFlag(t *testing.T) {
	if code, _, _ := invoke("validate", "--source", "ena:DRR021374", t.TempDir()); code != verifier.ExitUsage {
		t.Fatalf("exit %d", code)
	}
}

func TestSourceCertificatesStillAgreeWithTheIndependentVerifier(t *testing.T) {
	exe := exttool.Python(t)
	spec := specOf(t, "DRR021374")
	files := syntheticRun(spec)
	files["DRR021374_2.fastq.gz"] = shortenReads(files["DRR021374_2.fastq.gz"])
	dir := t.TempDir()
	writeDecompressed(t, dir, files)
	code, out, _ := checkSource(t, &fakeTransport{body: recorded(t, spec.accession)}, "--source", "ena:"+spec.accession, "--certificate", dir)
	if code != verifier.ExitIncomplete {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, name := range []string{"DRR021374_1.fastq", "DRR021374_2.fastq"} {
		data := filepath.Join(dir, name)
		cert := data + ".colligo-cert.json"
		result, err := exec.Command(exe, filepath.Join("..", "..", "tools", "verify_cert.py"), cert, data).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: the independent verifier disagrees: %v\n%s", name, err, result)
		}
		if !bytes.Contains(result, []byte("UNCHECKED assertion source_run_failed")) {
			t.Errorf("%s: the run failure must be listed as not recomputed:\n%s", name, result)
		}
	}
}
