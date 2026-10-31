package model

type Experiment struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

// RunCounts are the totals the source declares for a whole run, summed over
// every file of the run. They are not per-file values.
type RunCounts struct {
	ReadCount int64 `json:"read_count"`
	BaseCount int64 `json:"base_count"`
}

// Sample is one run as the source describes it. Layout is the source's own
// label and is not verified against the files.
type Sample struct {
	ID           string     `json:"id"`
	ExperimentID string     `json:"experiment_id"`
	Accession    string     `json:"accession"`
	Layout       string     `json:"layout"`
	SourceStatus string     `json:"source_status"`
	Declared     RunCounts  `json:"declared"`
	Artifacts    []Artifact `json:"artifacts"`
}
