// Package format holds the structural validators. Each one also extracts
// facts, from the single read of the file, without ever opening it.
package format

import (
	"io"

	"github.com/colligo-go/colligo/model"
)

type Extractor interface {
	io.Writer
	Finish() ([]model.Fact, error)
}

type FormatValidator interface {
	Format() string
	Version() string
	MagicPrefix() []byte
	Compressed() bool
	NewExtractor() Extractor
}

const (
	ContainerNone = "none"
	ContainerGzip = "gzip"
	ContainerBgzf = "bgzf"
)

const (
	ContentFastq   = "fastq"
	ContentFasta   = "fasta"
	ContentGTF     = "gtf"
	ContentHTML    = "html"
	ContentUnknown = "unknown"
)

func Registry() []FormatValidator {
	return []FormatValidator{gzipValidator{}, bgzfValidator{}, fastqValidator{}, fastaValidator{}, gtfValidator{}}
}

func Lookup(label string) (FormatValidator, bool) {
	for _, v := range Registry() {
		if v.Format() == label {
			return v, true
		}
	}
	return nil, false
}

func Known(label string) bool {
	_, ok := Lookup(label)
	return ok
}
