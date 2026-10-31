package format

import (
	"bytes"
	"fmt"

	"github.com/colligo-go/colligo/identity"
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

func (v gtfValidator) NewIdentifyingExtractor() Extractor {
	g := newGTFExtractor()
	g.observer = newGTFObserver()
	return rejectBOM(ContentGTF, v.Version(), &identifyingGTF{inner: g})
}

type identifyingGTF struct {
	inner *gtfExtractor
}

func (g *identifyingGTF) Write(p []byte) (int, error) { return g.inner.Write(p) }

func (g *identifyingGTF) Finish() ([]model.Fact, error) {
	found, err := g.inner.Finish()
	if err != nil || g.inner.bad != nil {
		return found, err
	}
	header, err := jsonFact(identity.GTFHeaderKind, identity.GTFHeaderVersion, g.inner.observer.header)
	if err != nil {
		return nil, err
	}
	return append(found, header), nil
}

type gtfExtractor struct {
	observer *gtfObserver
	lines    lineReader
	names    nameSampler
	records  int64
	bad      *problem
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
	if g.bad != nil {
		return
	}
	if length == 0 || line[0] == '#' {
		if g.observer != nil {
			g.observer.comment(line, length)
		}
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
	if g.observer != nil {
		g.observer.feature(line)
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
