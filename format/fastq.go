package format

import (
	"fmt"

	"github.com/colligo-go/colligo/model"
)

const maxLengthBuckets = 4096

type fastqValidator struct{}

func (fastqValidator) Format() string          { return ContentFastq }
func (fastqValidator) Version() string         { return "fastq-2" }
func (fastqValidator) MagicPrefix() []byte     { return []byte{'@'} }
func (fastqValidator) Compressed() bool        { return false }
func (fastqValidator) NewExtractor() Extractor { return newFastqExtractor() }

type fastqExtractor struct {
	lines    lineReader
	state    uint8
	seqLen   int
	records  int64
	bases    int64
	lengths  map[int]int64
	overflow int64
	first    []byte
	last     []byte
	hasID    bool
	blank    int64
	blankAt  int64
	bad      *problem
}

func newFastqExtractor() *fastqExtractor {
	f := &fastqExtractor{lengths: make(map[int]int64)}
	f.lines.handle = f.line
	return f
}

func (f *fastqExtractor) Write(p []byte) (int, error) {
	if f.bad == nil {
		f.lines.write(p)
	}
	return len(p), nil
}

func (f *fastqExtractor) fail(reason string, offset int64) {
	f.bad = &problem{reason: reason, offset: offset}
}

func (f *fastqExtractor) line(line []byte, length int, offset int64) {
	if f.bad != nil {
		return
	}
	switch f.state {
	case 0:
		if length == 0 {
			if f.blank == 0 {
				f.blankAt = offset
			}
			f.blank++
			return
		}
		if f.blank > 0 {
			f.fail("blank line inside the file", f.blankAt)
			return
		}
		if line[0] != '@' {
			f.fail("record does not start with '@'", offset)
			return
		}
		f.recordID(line[1:])
		f.state = 1
	case 1:
		f.seqLen = length
		f.state = 2
	case 2:
		if length == 0 || line[0] != '+' {
			f.fail("separator line does not start with '+'", offset)
			return
		}
		f.state = 3
	default:
		if length != f.seqLen {
			f.fail(fmt.Sprintf("quality length %d differs from sequence length %d", length, f.seqLen), offset)
			return
		}
		f.records++
		f.bases += int64(f.seqLen)
		if _, seen := f.lengths[f.seqLen]; seen || len(f.lengths) < maxLengthBuckets {
			f.lengths[f.seqLen]++
		} else {
			f.overflow++
		}
		f.state = 0
	}
}

func (f *fastqExtractor) recordID(header []byte) {
	end := len(header)
	for i, c := range header {
		if c == ' ' || c == '\t' {
			end = i
			break
		}
	}
	id := header[:end]
	if n := len(id); n > 0 && id[n-1] == '\r' {
		id = id[:n-1]
	}
	if n := len(id); n >= 2 && id[n-2] == '/' && (id[n-1] == '1' || id[n-1] == '2') {
		id = id[:n-2]
	}
	if !f.hasID {
		f.first = append(f.first[:0], id...)
		f.hasID = true
	}
	f.last = append(f.last[:0], id...)
}

type recordsValue struct {
	Records  int64         `json:"records"`
	Bases    int64         `json:"bases"`
	Lengths  map[int]int64 `json:"length_histogram"`
	Overflow int64         `json:"length_overflow"`
}

type readEndsValue struct {
	First string `json:"first"`
	Last  string `json:"last"`
}

func (f *fastqExtractor) Finish() ([]model.Fact, error) {
	if f.bad == nil {
		switch {
		case f.lines.partial() > 0:
			f.fail("last line is incomplete", f.lines.pos)
		case f.state != 0:
			f.fail("line count is not a multiple of 4", f.lines.pos)
		case f.records == 0:
			f.fail("no records", 0)
		}
	}
	version := fastqValidator{}.Version()
	sv := structureValue{Format: ContentFastq, Valid: f.bad == nil, TrailingBlankLines: f.blank}
	if f.bad != nil {
		sv.Reason, sv.Offset, sv.TrailingBlankLines = f.bad.reason, f.bad.offset, 0
	}
	structure, err := jsonFact("structure", version, sv)
	if err != nil || f.bad != nil {
		return []model.Fact{structure}, err
	}
	records, err := jsonFact("records", version, recordsValue{Records: f.records, Bases: f.bases, Lengths: f.lengths, Overflow: f.overflow})
	if err != nil {
		return nil, err
	}
	ends, err := jsonFact("read_ends", version, readEndsValue{First: string(f.first), Last: string(f.last)})
	if err != nil {
		return nil, err
	}
	return []model.Fact{structure, records, ends}, nil
}
