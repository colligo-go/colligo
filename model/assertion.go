package model

// Assertion is a comparison computed on demand between facts, expected
// values and declarations. It has no table of its own.
type Assertion struct {
	ID          string   `json:"id"`
	Mode        string   `json:"mode"`
	Severity    Severity `json:"severity"`
	Passed      bool     `json:"passed"`
	Skipped     bool     `json:"skipped,omitempty"`
	SkippedBy   string   `json:"skipped_by,omitempty"`
	Declared    string   `json:"declared"`
	Observed    string   `json:"observed"`
	Observation string   `json:"observation,omitempty"`
	Cause       string   `json:"probable_cause,omitempty"`
	Action      string   `json:"suggested_action,omitempty"`
}
