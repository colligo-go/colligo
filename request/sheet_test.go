package request_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/request"
	"github.com/colligo-go/colligo/secret"
)

func TestASamplesheetBecomesFilesWithTheirLinesAndKeepsTheSheet(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "a_R1.fastq", fastqRead)
	put(t, dir, "a_R2.fastq", fastqRead)
	sheet := secret.NewName(put(t, dir, "samples.csv", []byte(
		"sample,fastq_1,fastq_2\nA,a_R1.fastq,a_R2.fastq\nA,a_R1.fastq,a_R2.fastq\nB,s3://bucket/b.fastq.gz,\n")))
	req, err := request.Resolve(context.Background(), request.Inputs{Samplesheet: &sheet})
	if err != nil {
		t.Fatal(err)
	}
	if req.Sheet == nil || len(req.Sheet.Rows) != 3 {
		t.Fatalf("the sheet was not kept: %+v", req.Sheet)
	}
	if got := strings.Join(bases(req.Files), ","); got != "a_R1.fastq,a_R2.fastq" {
		t.Errorf("files %s: a remote entry is not a file and a repeated one is listed once", got)
	}
	if lines := req.Files[0].Lines(request.FromSheetLine); len(lines) != 2 || lines[0] != 2 || lines[1] != 3 {
		t.Errorf("lines %v", lines)
	}
}

func TestAMalformedSamplesheetIsAUsageError(t *testing.T) {
	dir := t.TempDir()
	sheet := secret.NewName(put(t, dir, "samples.csv", []byte("sample,reads\nA,a.fastq\n")))
	_, err := request.Resolve(context.Background(), request.Inputs{Samplesheet: &sheet})
	if msg := usageMessage(t, err); !strings.Contains(msg, "samplesheet") {
		t.Errorf("message %q", msg)
	}
}

func TestAFormWithoutLinesHasNoLinesToReport(t *testing.T) {
	file := request.File{Name: secret.NewName("a"), Origins: []request.Origin{{Kind: request.FromArgument}}}
	if lines := file.Lines(request.FromListLine); len(lines) != 0 {
		t.Errorf("lines %v", lines)
	}
	for kind, want := range map[request.Origin]string{
		{Kind: request.FromArgument}:            "argument",
		{Kind: request.FromFolder}:              "folder",
		{Kind: request.FromListLine, Line: 4}:   "list line 4",
		{Kind: request.FromSheetLine, Line: 12}: "samplesheet line 12",
	} {
		if kind.String() != want {
			t.Errorf("%+v is %q, want %q", kind, kind.String(), want)
		}
	}
	for lines, want := range map[string][]int{"line 3": {3}, "lines 3 and 7": {7, 3}, "lines 1, 2 and 5": {5, 1, 2}} {
		if got := request.DescribeLines(want); got != lines {
			t.Errorf("DescribeLines(%v) = %q, want %q", want, got, lines)
		}
	}
}
