package identity

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Checksum is an asset published by an official source, named as the source
// names it, with the MD5 the source published for it. Origin is the address the digest was read from and
// Retrieved the day it was read: an entry without them does not belong in the
// table.
type Checksum struct {
	Label     string `json:"name"`
	Asset     string `json:"file"`
	MD5       string `json:"md5"`
	Origin    string `json:"origin"`
	Retrieved string `json:"retrieved"`
}

// Table is everything identify compares a file against.
type Table struct {
	Collections []Reference
	Checksums   []Checksum
}

//go:embed checksums.json
var embeddedChecksums []byte

var md5Hex = regexp.MustCompile(`^[0-9a-f]{32}$`)

// TableError means an entry of a checksum table cannot be trusted.
type TableError struct {
	Index   int
	Problem string
}

func (e *TableError) Error() string {
	return fmt.Sprintf("identity: checksum table entry %d: %s", e.Index, e.Problem)
}

// ParseChecksums reads and validates a checksum table. Every entry needs a
// name, a file, a lowercase MD5, an https origin and a retrieval date, and no
// MD5 may appear twice.
func ParseChecksums(data []byte) ([]Checksum, error) {
	var entries []Checksum
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("identity: checksum table: %w", err)
	}
	seen := make(map[string]int, len(entries))
	for i, e := range entries {
		if problem := entryProblem(e); problem != "" {
			return nil, &TableError{Index: i, Problem: problem}
		}
		if first, dup := seen[e.MD5]; dup {
			return nil, &TableError{Index: i, Problem: fmt.Sprintf("md5 %s repeats entry %d", e.MD5, first)}
		}
		seen[e.MD5] = i
	}
	return entries, nil
}

func entryProblem(e Checksum) string {
	switch {
	case strings.TrimSpace(e.Label) == "":
		return "no name"
	case strings.TrimSpace(e.Asset) == "":
		return "no file"
	case !md5Hex.MatchString(e.MD5):
		return "md5 is not 32 lowercase hexadecimal digits"
	case !strings.HasPrefix(e.Origin, "https://"):
		return "origin is not an https address"
	}
	if _, err := time.Parse(time.DateOnly, e.Retrieved); err != nil {
		return "retrieved is not a date (YYYY-MM-DD)"
	}
	return ""
}

// KnownChecksums returns the embedded table, validated.
func KnownChecksums() ([]Checksum, error) {
	return ParseChecksums(embeddedChecksums)
}

// Embedded returns everything identify knows out of the box.
func Embedded() (Table, error) {
	sums, err := KnownChecksums()
	if err != nil {
		return Table{}, err
	}
	return Table{Collections: Known(), Checksums: sums}, nil
}

// RecognizeChecksum returns the entry whose MD5 equals the given lowercase
// hexadecimal one. The name of the file never takes part.
func RecognizeChecksum(md5 string, entries []Checksum) (Checksum, bool) {
	for _, e := range entries {
		if e.MD5 == md5 {
			return e, true
		}
	}
	return Checksum{}, false
}
