package format

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/colligo-go/colligo/secret"
)

var pairSuffix = regexp.MustCompile(`^(.*?)(_R|_|\.)([12])(_\d{3})?$`)

// PairKey splits a read file name into the stem shared by both mates and the
// mate number (1 or 2), recognising _R1/_R2, _1/_2 and .1/.2 suffixes.
func PairKey(name secret.Name) (stem string, mate int, ok bool) {
	full := name.String()
	base := filepath.Base(full)
	lower := strings.ToLower(base)
	for _, ext := range []string{".gz", ".bgz"} {
		if strings.HasSuffix(lower, ext) {
			base = base[:len(base)-len(ext)]
			lower = lower[:len(lower)-len(ext)]
		}
	}
	for _, ext := range []string{".fastq", ".fq"} {
		if strings.HasSuffix(lower, ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	m := pairSuffix.FindStringSubmatch(base)
	if m == nil || m[1] == "" {
		return "", 0, false
	}
	mate = int(m[3][0] - '0')
	return filepath.Join(filepath.Dir(full), m[1]+m[2]+"#"+m[4]), mate, true
}
