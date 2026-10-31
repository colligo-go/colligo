// Package samplesheet reads the CSV samplesheets Colligo checks: the
// nf-core/rnaseq format and the generic sample,fastq_1,fastq_2 format. It
// parses and resolves paths; it never opens the data files.
package samplesheet

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/colligo-go/colligo/internal/fsaccess"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
)

const (
	LayoutRnaseq  = "nf-core/rnaseq"
	LayoutGeneric = "generic"

	columnSample       = "sample"
	columnFastq1       = "fastq_1"
	columnFastq2       = "fastq_2"
	columnStrandedness = "strandedness"
)

var (
	utf8BOM          = []byte{0xEF, 0xBB, 0xBF}
	genericColumns   = []string{columnSample, columnFastq1, columnFastq2}
	rnaseqRequired   = []string{columnSample, columnFastq1, columnStrandedness}
	rnaseqOptional   = []string{columnFastq2, "seq_platform", "seq_center", "genome_bam", "transcriptome_bam", "percent_mapped"}
	strandednessEnum = []string{"forward", "reverse", "unstranded", "auto"}
	sampleName       = regexp.MustCompile(`^\S+$`)
	remoteScheme     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)
)

// FormatError describes a samplesheet that cannot be read as one of the
// supported formats. It is a usage error: nothing was checked. Line is the
// 1-based line of the file, or 0 when the problem is not tied to one. It
// never carries a path or the content of a field.
type FormatError struct {
	Line   int
	Reason string
}

func (e *FormatError) Error() string {
	if e.Line == 0 {
		return "samplesheet: " + e.Reason
	}
	return fmt.Sprintf("samplesheet line %d: %s", e.Line, e.Reason)
}

func (e *FormatError) Severity() model.Severity { return model.Fatal }

// Reference is the value of a fastq column: empty, a local file or a remote
// location. A relative local path is already resolved against the directory
// of the samplesheet.
type Reference struct {
	name     secret.Name
	endpoint secret.Endpoint
	kind     referenceKind
}

type referenceKind int

const (
	kindEmpty referenceKind = iota
	kindLocal
	kindRemote
)

func (r Reference) Set() bool                 { return r.kind != kindEmpty }
func (r Reference) Remote() bool              { return r.kind == kindRemote }
func (r Reference) Name() secret.Name         { return r.name }
func (r Reference) Endpoint() secret.Endpoint { return r.endpoint }

// Row is one data line. Line is its 1-based position in the file, counting
// the header as line 1, so it matches what an editor shows.
type Row struct {
	Line   int
	Sample string
	Fastq1 Reference
	Fastq2 Reference
}

type Sheet struct {
	Layout string
	Rows   []Row
}

// Read opens the samplesheet and parses it. Relative paths in it are
// resolved against the directory that holds the samplesheet.
func Read(name secret.Name) (Sheet, error) {
	f, err := fsaccess.Open(name.Raw)
	if err != nil {
		return Sheet{}, err
	}
	defer f.Close()
	return Parse(f, name)
}

// Parse reads a samplesheet from r. sheet is where it lives and only
// anchors relative paths. A malformed file is a *FormatError.
func Parse(r io.Reader, sheet secret.Name) (Sheet, error) {
	br := bufio.NewReader(r)
	if head, err := br.Peek(len(utf8BOM)); err == nil && bytes.Equal(head, utf8BOM) {
		if _, err := br.Discard(len(utf8BOM)); err != nil {
			return Sheet{}, err
		}
	}
	reader := csv.NewReader(br)
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return Sheet{}, &FormatError{Reason: "the file is empty"}
	}
	if err != nil {
		return Sheet{}, csvFailure(err)
	}
	layout, columns, err := resolveHeader(header)
	if err != nil {
		return Sheet{}, err
	}
	out := Sheet{Layout: layout}
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Sheet{}, csvFailure(err)
		}
		line, _ := reader.FieldPos(0)
		row, err := buildRow(line, record, columns, layout, sheet)
		if err != nil {
			return Sheet{}, err
		}
		out.Rows = append(out.Rows, row)
	}
	if len(out.Rows) == 0 {
		return Sheet{}, &FormatError{Line: 1, Reason: "the samplesheet has a header and no samples"}
	}
	return out, nil
}

func csvFailure(err error) error {
	var parse *csv.ParseError
	if errors.As(err, &parse) {
		return &FormatError{Line: parse.StartLine, Reason: "malformed CSV: " + parse.Err.Error()}
	}
	return err
}

func resolveHeader(header []string) (layout string, columns map[string]int, err error) {
	columns = make(map[string]int, len(header))
	for i, h := range header {
		h = strings.TrimSpace(h)
		if _, dup := columns[h]; dup {
			return "", nil, &FormatError{Line: 1, Reason: fmt.Sprintf("the column %q appears twice", h)}
		}
		columns[h] = i
	}
	if _, ok := columns[columnStrandedness]; ok {
		return LayoutRnaseq, columns, checkRnaseqHeader(columns)
	}
	if err := checkGenericHeader(columns); err != nil {
		return "", nil, err
	}
	return LayoutGeneric, columns, nil
}

func checkRnaseqHeader(columns map[string]int) error {
	for _, want := range rnaseqRequired {
		if _, ok := columns[want]; !ok {
			return &FormatError{Line: 1, Reason: fmt.Sprintf("the %s columns need %q", LayoutRnaseq, want)}
		}
	}
	for name := range columns {
		if !slices.Contains(rnaseqRequired, name) && !slices.Contains(rnaseqOptional, name) {
			return &FormatError{Line: 1, Reason: fmt.Sprintf("the column %q is not part of the %s samplesheet", name, LayoutRnaseq)}
		}
	}
	return nil
}

func checkGenericHeader(columns map[string]int) error {
	for _, want := range genericColumns {
		if _, ok := columns[want]; !ok {
			return &FormatError{Line: 1, Reason: fmt.Sprintf(
				"unknown samplesheet format: the header needs %s (or the %s columns with strandedness)",
				strings.Join(genericColumns, ","), LayoutRnaseq)}
		}
	}
	if len(columns) != len(genericColumns) {
		return &FormatError{Line: 1, Reason: fmt.Sprintf(
			"unknown samplesheet format: the generic header is exactly %s", strings.Join(genericColumns, ","))}
	}
	return nil
}

func buildRow(line int, record []string, columns map[string]int, layout string, sheet secret.Name) (Row, error) {
	field := func(column string) string {
		i, ok := columns[column]
		if !ok {
			return ""
		}
		return strings.TrimSpace(record[i])
	}
	row := Row{Line: line, Sample: field(columnSample)}
	if !sampleName.MatchString(row.Sample) {
		return Row{}, &FormatError{Line: line, Reason: "the sample name must be present and cannot contain spaces"}
	}
	if layout == LayoutRnaseq && !slices.Contains(strandednessEnum, field(columnStrandedness)) {
		return Row{}, &FormatError{Line: line, Reason: "strandedness must be one of " + strings.Join(strandednessEnum, ", ")}
	}
	var err error
	if row.Fastq1, err = reference(line, columnFastq1, field(columnFastq1), sheet); err != nil {
		return Row{}, err
	}
	if !row.Fastq1.Set() {
		return Row{}, &FormatError{Line: line, Reason: "fastq_1 must be present"}
	}
	if row.Fastq2, err = reference(line, columnFastq2, field(columnFastq2), sheet); err != nil {
		return Row{}, err
	}
	return row, nil
}

func reference(line int, column, raw string, sheet secret.Name) (Reference, error) {
	switch {
	case raw == "":
		return Reference{}, nil
	case remoteScheme.MatchString(raw):
		endpoint, err := secret.NewEndpoint(raw)
		if err != nil {
			return Reference{}, &FormatError{Line: line, Reason: column + " is not a valid URL"}
		}
		return Reference{endpoint: endpoint, kind: kindRemote}, nil
	case filepath.IsAbs(raw):
		return Reference{name: secret.NewName(filepath.Clean(raw)), kind: kindLocal}, nil
	}
	return Reference{name: secret.NewName(fsaccess.Resolve(sheet.Raw, raw)), kind: kindLocal}, nil
}
