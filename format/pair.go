package format

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/colligo-go/colligo/secret"
)

var pairSuffix = regexp.MustCompile(`^(.*?)(_R|_|\.)([12])(_\d{3})?$`)

func splitMate(base string) (match []string, extensions string) {
	lower := strings.ToLower(base)
	end := len(base)
	for _, ext := range []string{".gz", ".bgz"} {
		if strings.HasSuffix(lower[:end], ext) {
			end -= len(ext)
		}
	}
	for _, ext := range []string{".fastq", ".fq"} {
		if strings.HasSuffix(lower[:end], ext) {
			end -= len(ext)
			break
		}
	}
	m := pairSuffix.FindStringSubmatch(base[:end])
	if m == nil || m[1] == "" {
		return nil, ""
	}
	return m, base[end:]
}

func PairKey(name secret.Name) (stem string, mate int, ok bool) {
	full := name.String()
	m, _ := splitMate(filepath.Base(full))
	if m == nil {
		return "", 0, false
	}
	mate = int(m[3][0] - '0')
	return filepath.Join(filepath.Dir(full), m[1]+m[2]+"#"+m[4]), mate, true
}

// MateName returns the base name, without the directory, that the other mate
// would have: name's stem, chunk number and extensions around the flipped
// mate digit. ok is false when name carries no mate suffix.
func MateName(name secret.Name) (string, bool) {
	m, extensions := splitMate(filepath.Base(name.String()))
	if m == nil {
		return "", false
	}
	other := "1"
	if m[3] == "1" {
		other = "2"
	}
	return m[1] + m[2] + other + m[4] + extensions, true
}
