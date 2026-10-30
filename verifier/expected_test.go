package verifier

import (
	"strings"
	"testing"
)

var (
	md5a = strings.Repeat("a", 32)
	md5b = strings.Repeat("b", 32)
)

func TestParseExpectedRejectsAnyRepeatedKeyNamingTheValues(t *testing.T) {
	for _, pairs := range [][]string{
		{"md5=" + md5a, "md5=" + md5b},
		{"md5=" + md5a, "MD5=" + md5a},
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
	if _, err := ParseExpected([]string{"md5=" + md5a, "size=1"}); err != nil {
		t.Fatal(err)
	}
}

func TestParseExpectedRejectsMalformedHashes(t *testing.T) {
	for _, pair := range []string{
		"md5=" + strings.Repeat("a", 31),
		"md5=" + strings.Repeat("a", 33),
		"md5=" + strings.Repeat("a", 31) + "g",
		"sha256=" + strings.Repeat("a", 32),
		"sha256=" + strings.Repeat("a", 63) + "z",
		"blake3=" + strings.Repeat("a", 63),
		"blake3=" + strings.Repeat("a", 65),
		"md5=" + strings.Repeat("a", 31) + " ",
	} {
		if _, err := ParseExpected([]string{pair}); err == nil || !strings.Contains(err.Error(), "hexadecimal") {
			t.Errorf("%q: %v", pair, err)
		}
	}
}

func TestParseExpectedAcceptsHashesOfAnyCase(t *testing.T) {
	exp, err := ParseExpected([]string{
		"md5=" + strings.Repeat("aB", 16),
		"sha256=" + strings.Repeat("0F", 32),
		"blake3=" + strings.Repeat("9e", 32),
	})
	if err != nil || exp.MD5 == "" || exp.SHA256 == "" || exp.BLAKE3 == "" {
		t.Fatalf("%+v %v", exp, err)
	}
}
