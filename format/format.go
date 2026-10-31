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

// Identifying is implemented by a validator whose extractor can also derive
// identity facts from the same stream, such as the seqcol digest of a FASTA.
type Identifying interface {
	NewIdentifyingExtractor() Extractor
}

// ExtractorFor returns the identifying extractor when identify is set and the
// validator offers one, and the plain extractor otherwise.
func ExtractorFor(v FormatValidator, identify bool) Extractor {
	if i, ok := v.(Identifying); ok && identify {
		return i.NewIdentifyingExtractor()
	}
	return v.NewExtractor()
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
