package format

import (
	"bytes"
	"fmt"

	"github.com/colligo-go/colligo/model"
)

type gtfValidator struct{}

func (gtfValidator) Format() string      { return ContentGTF }
func (gtfValidator) Version() string     { return "gtf-2" }
func (gtfValidator) MagicPrefix() []byte { return nil }
func (gtfValidator) Compressed() bool    { return false }
func (v gtfValidator) NewExtractor() Extractor {
	return rejectBOM(ContentGTF, v.Version(), newGTFExtractor())
}

type gtfExtractor struct {
	lines   lineReader
	names   nameSampler
	records int64
	bad     *problem
}

func newGTFExtractor() *gtfExtractor {
	g := &gtfExtractor{}
	g.lines.handle = g.line
	return g
}

func (g *gtfExtractor) Write(p []byte) (int, error) {
	if g.bad == nil {
		g.lines.write(p)
	}
	return len(p), nil
}

func (g *gtfExtractor) line(line []byte, length int, offset int64) {
	if g.bad != nil || length == 0 || line[0] == '#' {
		return
	}
	if length > len(line) {
		g.bad = &problem{reason: fmt.Sprintf("feature line is longer than %d bytes", maxKeptLine), offset: offset}
		return
	}
	if n := bytes.Count(line, []byte{'\t'}) + 1; n != 9 {
		g.bad = &problem{reason: fmt.Sprintf("feature line has %d tab-separated fields, want 9", n), offset: offset}
		return
	}
	g.names.add(line[:bytes.IndexByte(line, '\t')])
	g.records++
}

func (g *gtfExtractor) Finish() ([]model.Fact, error) {
	if g.bad == nil {
		switch {
		case g.lines.partial() > 0:
			g.bad = &problem{reason: "last line is incomplete", offset: g.lines.pos}
		case g.records == 0:
			g.bad = &problem{reason: "no feature lines", offset: 0}
		}
	}
	version := gtfValidator{}.Version()
	structure, err := structureFact(version, ContentGTF, g.bad)
	if err != nil || g.bad != nil {
		return []model.Fact{structure}, err
	}
	records, err := jsonFact("records", version, struct {
		Records int64 `json:"records"`
	}{g.records})
	if err != nil {
		return nil, err
	}
	names, err := g.names.fact(version)
	if err != nil {
		return nil, err
	}
	return []model.Fact{structure, records, names}, nil
}
