package format

import (
	"bytes"
	"encoding/json"

	"github.com/colligo-go/colligo/model"
)

// PayloadReporter is implemented by container extractors. The pipeline that
// decompresses the payload reports how it ended, because only it sees the
// decompressed stream.
type PayloadReporter interface {
	ReportPayload(consumed, decompressed int64, err error)
}

type gzipValidator struct{}

func (gzipValidator) Format() string      { return ContainerGzip }
func (gzipValidator) Version() string     { return "gzip-1" }
func (gzipValidator) MagicPrefix() []byte { return append([]byte(nil), gzipMagic...) }
func (gzipValidator) Compressed() bool    { return true }
func (v gzipValidator) NewExtractor() Extractor {
	return &containerExtractor{label: v.Format(), version: v.Version()}
}

type bgzfValidator struct{}

func (bgzfValidator) Format() string      { return ContainerBgzf }
func (bgzfValidator) Version() string     { return "bgzf-1" }
func (bgzfValidator) MagicPrefix() []byte { return append([]byte(nil), gzipMagic...) }
func (bgzfValidator) Compressed() bool    { return true }
func (v bgzfValidator) NewExtractor() Extractor {
	return &containerExtractor{label: v.Format(), version: v.Version(), bgzf: true}
}

type containerExtractor struct {
	label        string
	version      string
	bgzf         bool
	size         int64
	tail         [28]byte
	tailLen      int
	consumed     int64
	decompressed int64
	payloadErr   error
	reported     bool
}

func (c *containerExtractor) Write(p []byte) (int, error) {
	c.size += int64(len(p))
	if len(p) >= len(c.tail) {
		copy(c.tail[:], p[len(p)-len(c.tail):])
		c.tailLen = len(c.tail)
		return len(p), nil
	}
	if c.tailLen+len(p) > len(c.tail) {
		drop := c.tailLen + len(p) - len(c.tail)
		copy(c.tail[:], c.tail[drop:c.tailLen])
		c.tailLen -= drop
	}
	copy(c.tail[c.tailLen:], p)
	c.tailLen += len(p)
	return len(p), nil
}

func (c *containerExtractor) ReportPayload(consumed, decompressed int64, err error) {
	c.consumed, c.decompressed, c.payloadErr, c.reported = consumed, decompressed, err, true
}

type containerValue struct {
	structureValue
	Uncompressed int64 `json:"uncompressed_bytes"`
}

func (c *containerExtractor) Finish() ([]model.Fact, error) {
	v := containerValue{structureValue: structureValue{Format: c.label, Valid: true}, Uncompressed: c.decompressed}
	switch {
	case c.reported && c.payloadErr != nil:
		v.Valid, v.Reason, v.Offset = false, c.payloadErr.Error(), c.consumed
	case c.bgzf && !bytes.Equal(c.tail[:c.tailLen], bgzfEOF):
		v.Valid, v.Reason, v.Offset = false, "missing BGZF EOF block", c.size
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return []model.Fact{{Kind: "structure", ExtractorVersion: c.version, Value: raw}}, nil
}
