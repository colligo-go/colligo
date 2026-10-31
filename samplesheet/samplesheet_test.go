package samplesheet

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/secret"
)

var sheetLocation = secret.NewName(filepath.Join("study", "inputs", "samples.csv"))

func parse(t *testing.T, text string) Sheet {
	t.Helper()
	sheet, err := Parse(strings.NewReader(text), sheetLocation)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return sheet
}

func formatError(t *testing.T, text string) *FormatError {
	t.Helper()
	_, err := Parse(strings.NewReader(text), sheetLocation)
	var fe *FormatError
	if !errors.As(err, &fe) {
		t.Fatalf("want a *FormatError, got %v", err)
	}
	return fe
}

func TestParseRnaseqSamplesheet(t *testing.T) {
	sheet := parse(t, "sample,fastq_1,fastq_2,strandedness\n"+
		"WT_REP1,a_R1.fastq.gz,a_R2.fastq.gz,auto\n"+
		"WT_REP1,b_R1.fastq.gz,b_R2.fastq.gz,auto\n"+
		"KO_REP1,c.fastq.gz,,reverse\n")
	if sheet.Layout != LayoutRnaseq || len(sheet.Rows) != 3 {
		t.Fatalf("layout %q, %d rows", sheet.Layout, len(sheet.Rows))
	}
	if sheet.Rows[0].Line != 2 || sheet.Rows[2].Line != 4 {
		t.Fatalf("lines %d and %d, want 2 and 4", sheet.Rows[0].Line, sheet.Rows[2].Line)
	}
	if sheet.Rows[2].Fastq2.Set() {
		t.Fatal("an empty fastq_2 must stay empty (single-end)")
	}
	if sheet.Rows[0].Sample != sheet.Rows[1].Sample {
		t.Fatal("lanes of one sample keep the same name")
	}
}

func TestParseGenericSamplesheetInAnyColumnOrder(t *testing.T) {
	sheet := parse(t, "fastq_2,sample,fastq_1\nr2.fq,S1,r1.fq\n")
	row := sheet.Rows[0]
	if sheet.Layout != LayoutGeneric || row.Sample != "S1" {
		t.Fatalf("layout %q sample %q", sheet.Layout, row.Sample)
	}
	if got := filepath.Base(row.Fastq1.Name().String()); got != "r1.fq" {
		t.Fatalf("fastq_1 resolved to %q", got)
	}
}

func TestRnaseqAcceptsItsOptionalColumnsAndAMissingFastq2Column(t *testing.T) {
	sheet := parse(t, "sample,fastq_1,strandedness,seq_platform,seq_center,percent_mapped\nS1,r1.fq,forward,ILLUMINA,Centre,98.5\n")
	if sheet.Layout != LayoutRnaseq || sheet.Rows[0].Fastq2.Set() {
		t.Fatalf("layout %q", sheet.Layout)
	}
}

func TestRelativePathsResolveAgainstTheSamplesheetDirectory(t *testing.T) {
	sheet := parse(t, "sample,fastq_1,fastq_2\nS1,reads/r1.fq,/data/r2.fq\n")
	row := sheet.Rows[0]
	if got, want := row.Fastq1.Name().String(), filepath.Join("study", "inputs", "reads", "r1.fq"); got != want {
		t.Fatalf("relative path resolved to %q, want %q", got, want)
	}
	if got, want := row.Fastq2.Name().String(), "/data/r2.fq"; got != want {
		t.Fatalf("absolute path became %q, want %q", got, want)
	}
}

func TestRemoteLocationIsKeptAsAnEndpointNotAPath(t *testing.T) {
	sheet := parse(t, "sample,fastq_1,fastq_2\nS1,s3://bucket/r1.fq.gz,https://host.example/r2.fq.gz?token=SECRET\n")
	row := sheet.Rows[0]
	if !row.Fastq1.Remote() || !row.Fastq2.Remote() {
		t.Fatal("s3:// and https:// must be remote")
	}
	if strings.Contains(row.Fastq2.Endpoint().String(), "SECRET") {
		t.Fatalf("the query string leaked: %s", row.Fastq2.Endpoint())
	}
}

func TestParseToleratesByteOrderMarkAndQuotedFields(t *testing.T) {
	sheet := parse(t, "\xEF\xBB\xBFsample,fastq_1,fastq_2\n\"S1\",\"dir with space/r1.fq\",\n")
	if sheet.Rows[0].Sample != "S1" || !strings.HasSuffix(sheet.Rows[0].Fastq1.Name().String(), "dir with space/r1.fq") {
		t.Fatalf("row %+v", sheet.Rows[0])
	}
}

func TestMalformedSamplesheetsAreUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		text string
		line int
		want string
	}{
		{"empty file", "", 0, "empty"},
		{"header only", "sample,fastq_1,fastq_2\n", 1, "no samples"},
		{"generic without fastq_2", "sample,fastq_1\nS1,a.fq\n", 1, "unknown samplesheet format"},
		{"generic without sample", "fastq_1,fastq_2\na.fq,b.fq\n", 1, "unknown samplesheet format"},
		{"generic with an extra column", "sample,fastq_1,fastq_2,replicate\nS1,a.fq,b.fq,1\n", 1, "exactly"},
		{"rnaseq without fastq_1", "sample,fastq_2,strandedness\nS1,b.fq,auto\n", 1, `need "fastq_1"`},
		{"rnaseq with an unknown column", "sample,fastq_1,strandedness,patient\nS1,a.fq,auto,P1\n", 1, `"patient"`},
		{"duplicated column", "sample,fastq_1,fastq_1\nS1,a.fq,b.fq\n", 1, "twice"},
		{"short row", "sample,fastq_1,fastq_2\nS1,a.fq,b.fq\nS2,c.fq\n", 3, "malformed CSV"},
		{"unterminated quote", "sample,fastq_1,fastq_2\nS1,\"a.fq,b.fq\n", 2, "malformed CSV"},
		{"empty sample", "sample,fastq_1,fastq_2\n,a.fq,b.fq\n", 2, "sample name"},
		{"space in sample", "sample,fastq_1,fastq_2\nS 1,a.fq,b.fq\n", 2, "sample name"},
		{"missing fastq_1", "sample,fastq_1,fastq_2\nS1,,b.fq\n", 2, "fastq_1 must be present"},
		{"bad strandedness", "sample,fastq_1,strandedness\nS1,a.fq,sideways\n", 2, "strandedness"},
		{"invalid url", "sample,fastq_1,fastq_2\nS1,https://[bad/a.fq,\n", 2, "not a valid URL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fe := formatError(t, c.text)
			if fe.Line != c.line || !strings.Contains(fe.Error(), c.want) {
				t.Fatalf("got line %d: %q, want line %d containing %q", fe.Line, fe, c.line, c.want)
			}
			if fe.Severity() == "" {
				t.Fatal("a typed error carries a severity")
			}
		})
	}
}

func TestFormatErrorNeverCarriesTheContentOfAField(t *testing.T) {
	for _, text := range []string{
		"sample,fastq_1,fastq_2\nS1,https://[bad/secret-token.fq,\n",
		"sample,fastq_1,fastq_2\nS1,\"secret-token.fq,b.fq\n",
		"sample,fastq_1,strandedness\nS1,a.fq,secret-token\n",
	} {
		if fe := formatError(t, text); strings.Contains(fe.Error(), "secret-token") {
			t.Fatalf("the error repeats a field: %s", fe)
		}
	}
}

func TestReadReportsAMissingSamplesheetAsNotExist(t *testing.T) {
	_, err := Read(secret.NewName(filepath.Join(t.TempDir(), "absent.csv")))
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
}

func TestLinesCountTheHeaderAndSkippedBlankLines(t *testing.T) {
	sheet := parse(t, "sample,fastq_1,fastq_2\n\nS1,a.fq,b.fq\n\n\nS2,c.fq,d.fq\n")
	got := fmt.Sprint(sheet.Rows[0].Line, sheet.Rows[1].Line)
	if got != "3 6" {
		t.Fatalf("lines %s, want 3 6", got)
	}
}
