package text

import (
	"github.com/colligo-go/colligo/render/theme"
)

var accessionReading = map[string]string{
	"link":       "taken from the link",
	"deduced":    "deduced from the file names",
	"normalized": "upper-cased from what was typed",
}

func inputBlock(v *view) []string {
	source := v.report.Source
	if source == nil || source.AccessionFrom == "" {
		return nil
	}
	reading, known := accessionReading[source.AccessionFrom]
	if !known {
		reading = source.AccessionFrom
	}
	return []string{v.theme.Apply(theme.Dim, "Input: accession "+source.Accession+", "+reading)}
}
