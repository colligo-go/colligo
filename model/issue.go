package model

// ValidationIssue is a defect in what the source returned, found without
// reading any file. Subject names the run or line the defect belongs to.
type ValidationIssue struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Subject  string   `json:"subject"`
	Message  string   `json:"message"`
}
