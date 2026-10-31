package model

import "github.com/colligo-go/colligo/secret"

// Artifact is one file the source lists. Read and base counts are not here:
// the source declares them per run, and they live in Sample.Declared.
type Artifact struct {
	URL          secret.Endpoint   `json:"url"`
	Name         secret.Name       `json:"name"`
	Size         int64             `json:"size"`
	MD5          string            `json:"md5,omitempty"`
	SHA256       string            `json:"sha256,omitempty"`
	Format       string            `json:"format"`
	PairID       string            `json:"pair_id,omitempty"`
	SourceStatus string            `json:"source_status,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
}
