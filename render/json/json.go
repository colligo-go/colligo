// Package json writes the machine-readable documents of Colligo. The
// styling of the text report never reaches it.
package json

import (
	stdjson "encoding/json"
	"io"

	"github.com/colligo-go/colligo/verifier"
)

type checkDocument struct {
	SchemaVersion int                    `json:"schema_version"`
	Files         []*verifier.FileReport `json:"files"`
	Skipped       []string               `json:"skipped,omitempty"`
	SkippedLinks  []string               `json:"skipped_directory_links,omitempty"`
	Source        *verifier.SourceReport `json:"source,omitempty"`
	ExitCode      int                    `json:"exit_code"`
}

type identifyDocument struct {
	SchemaVersion int                        `json:"schema_version"`
	Files         []*verifier.Identification `json:"files"`
	ExitCode      int                        `json:"exit_code"`
}

func Check(w io.Writer, r verifier.Report, exitCode int) error {
	return encode(w, checkDocument{
		SchemaVersion: verifier.SchemaVersion, Files: r.Files, Skipped: r.Skipped,
		SkippedLinks: r.SkippedLinks, Source: r.Source, ExitCode: exitCode})
}

func Identify(w io.Writer, r verifier.IdentifyReport) error {
	files := r.Files
	if files == nil {
		files = []*verifier.Identification{}
	}
	return encode(w, identifyDocument{SchemaVersion: verifier.IdentifySchemaVersion, Files: files, ExitCode: r.ExitCode()})
}

func encode(w io.Writer, document any) error {
	enc := stdjson.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(document)
}
