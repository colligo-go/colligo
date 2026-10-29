package format

import (
	"bytes"

	"github.com/colligo-go/colligo/model"
)

var utf8BOM = []byte{0xef, 0xbb, 0xbf}

type bomRejecter struct {
	inner   Extractor
	label   string
	version string
	head    [3]byte
	seen    int
}

func rejectBOM(label, version string, inner Extractor) Extractor {
	return &bomRejecter{inner: inner, label: label, version: version}
}

func (b *bomRejecter) Write(p []byte) (int, error) {
	if b.seen < len(b.head) {
		b.seen += copy(b.head[b.seen:], p)
	}
	return b.inner.Write(p)
}

func (b *bomRejecter) Finish() ([]model.Fact, error) {
	if bytes.Equal(b.head[:b.seen], utf8BOM) {
		fact, err := structureFact(b.version, b.label, &problem{reason: "UTF-8 byte order mark at the start of the file", offset: 0})
		return []model.Fact{fact}, err
	}
	return b.inner.Finish()
}
