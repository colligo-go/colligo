package format

import (
	"bytes"

	"github.com/colligo-go/colligo/model"
)

type fastaValidator struct{}

func (fastaValidator) Format() string          { return ContentFasta }
func (fastaValidator) Version() string         { return "fasta-2" }
func (fastaValidator) MagicPrefix() []byte     { return []byte{'>'} }
func (fastaValidator) Compressed() bool        { return false }
func (fastaValidator) NewExtractor() Extractor { return newFastaExtractor() }

type fastaExtractor struct {
	lines   lineReader
	names   nameSampler
	records int64
	bases   int64
	started bool
	bad     *problem
}

func newFastaExtractor() *fastaExtractor {
	f := &fastaExtractor{}
	f.lines.handle = f.line
	return f
}

func (f *fastaExtractor) Write(p []byte) (int, error) {
	if f.bad == nil {
		f.lines.write(p)
	}
	return len(p), nil
}

func (f *fastaExtractor) line(line []byte, length int, offset int64) {
	if f.bad != nil {
		return
	}
	if length > 0 && line[0] == ';' {
		f.bad = &problem{reason: "comment lines starting with ';' are not supported", offset: offset}
		return
	}
	if length > 0 && line[0] == '>' {
		name := bytes.Fields(line[1:])
		if len(name) == 0 {
			f.bad = &problem{reason: "record header has no name", offset: offset}
			return
		}
		f.names.add(name[0])
		f.records++
		f.started = true
		return
	}
	if !f.started {
		f.bad = &problem{reason: "sequence before the first header", offset: offset}
		return
	}
	f.bases += int64(length)
}

func (f *fastaExtractor) Finish() ([]model.Fact, error) {
	if f.bad == nil {
		switch {
		case f.lines.partial() > 0:
			f.bad = &problem{reason: "last sequence line is truncated: no trailing newline", offset: f.lines.pos}
		case f.records == 0:
			f.bad = &problem{reason: "no records", offset: 0}
		}
	}
	version := fastaValidator{}.Version()
	structure, err := structureFact(version, ContentFasta, f.bad)
	if err != nil || f.bad != nil {
		return []model.Fact{structure}, err
	}
	records, err := jsonFact("records", version, struct {
		Records int64 `json:"records"`
		Bases   int64 `json:"bases"`
	}{f.records, f.bases})
	if err != nil {
		return nil, err
	}
	names, err := f.names.fact(version)
	if err != nil {
		return nil, err
	}
	return []model.Fact{structure, records, names}, nil
}
