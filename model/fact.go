package model

import "encoding/json"

// Fact is a pure function of content: the same bytes and extractor version
// always produce the same Value.
type Fact struct {
	Kind             string          `json:"kind"`
	ExtractorVersion string          `json:"extractor_version"`
	Value            json.RawMessage `json:"value"`
}
