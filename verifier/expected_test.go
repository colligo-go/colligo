package verifier

import (
	"strings"
	"testing"
)

func TestParseExpectedRejectsAnyRepeatedKeyNamingTheValues(t *testing.T) {
	for _, pairs := range [][]string{
		{"md5=aa", "md5=bb"},
		{"md5=aa", "MD5=aa"},
		{"size=1", "Size=1"},
	} {
		_, err := ParseExpected(pairs)
		if err == nil {
			t.Errorf("%v: accepted", pairs)
			continue
		}
		key, _, _ := strings.Cut(strings.ToLower(pairs[0]), "=")
		if !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "more than once") {
			t.Errorf("%v: %v", pairs, err)
		}
	}
	if _, err := ParseExpected([]string{"md5=aa", "size=1"}); err != nil {
		t.Fatal(err)
	}
}
