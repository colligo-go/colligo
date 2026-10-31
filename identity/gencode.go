package identity

import (
	"regexp"
	"strings"
)

// GTFHeaderKind is the kind of the fact that carries what the header of a GTF
// declares about itself.
const GTFHeaderKind = "gtf_header"

// GTFHeaderVersion is the extractor version of the gtf_header fact.
const GTFHeaderVersion = "gtf-header-1"

// The limits of the header that is read. A header that outgrows either is
// flagged as truncated and the read goes on.
const (
	MaxHeaderLines = 1024
	MaxHeaderBytes = 1 << 20
)

// GTFHeader is the value of the gtf_header fact: the leading comment lines of
// a GTF, bounded, and the one content signal that tells GENCODE from Ensembl.
// It is a function of the bytes alone.
type GTFHeader struct {
	Description string `json:"description"`
	Provider    string `json:"provider"`
	Format      string `json:"format"`
	Date        string `json:"date"`
	HeaderLines int    `json:"header_lines"`
	// Truncated is true when the header outgrew its limits and was read only
	// up to them.
	Truncated bool `json:"header_truncated"`
	// GeneBiotypeLines counts the gene lines that carry the attribute
	// gene_biotype, which Ensembl writes and GENCODE does not.
	GeneBiotypeLines int64 `json:"gene_biotype_lines"`
}

// Gencode is what the header of a GTF declares about the release it comes
// from. It is a declaration: nothing in the file prevents an edited header.
type Gencode struct {
	Label    string `json:"label"`
	Release  string `json:"release"`
	Organism string `json:"organism"`
	Assembly string `json:"assembly,omitempty"`
	Ensembl  string `json:"ensembl,omitempty"`
	Date     string `json:"date,omitempty"`
}

var gencodeDescription = regexp.MustCompile(
	`^evidence-based annotation of the (\S+(?: \S+)*?) genome(?: \(([^)]+)\))?, version (M?[0-9]+)(?: \(Ensembl ([0-9]+)\))?$`)

// ParseGencode reads the ##description line of a GENCODE GTF, for example
// "evidence-based annotation of the human genome (GRCh38), version 44
// (Ensembl 110)". It reports false for any other line.
func ParseGencode(h GTFHeader) (Gencode, bool) {
	m := gencodeDescription.FindStringSubmatch(strings.TrimSpace(h.Description))
	if m == nil {
		return Gencode{}, false
	}
	return Gencode{
		Label:    "GENCODE " + m[3],
		Release:  m[3],
		Organism: m[1],
		Assembly: m[2],
		Ensembl:  m[4],
		Date:     h.Date,
	}, true
}

// NamesGencodeProvider reports whether the header names GENCODE as its
// provider, which says what the file claims to be even when the description
// carries no release.
func NamesGencodeProvider(h GTFHeader) bool {
	return strings.EqualFold(strings.TrimSpace(h.Provider), "GENCODE")
}
