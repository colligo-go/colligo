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

func TestParseExpectedReadsEveryCountKey(t *testing.T) {
	exp, err := ParseExpected([]string{"size=10", "reads=0", "bases=1234567890123"})
	if err != nil {
		t.Fatal(err)
	}
	if exp.Size == nil || *exp.Size != 10 || exp.Reads == nil || *exp.Reads != 0 || exp.Bases == nil || *exp.Bases != 1234567890123 {
		t.Fatalf("%+v", exp)
	}
}

func TestParseExpectedRejectsWhatIsNotACount(t *testing.T) {
	for _, pair := range []string{"bases=-1", "reads=1.5", "size=10k", "bases=99999999999999999999", "reads= 3"} {
		if _, err := ParseExpected([]string{pair}); err == nil || !strings.Contains(err.Error(), "is not a count") {
			t.Errorf("%q: %v", pair, err)
		}
	}
}

func TestParseExpectedNamesTheKnownKeysOnAnUnknownOne(t *testing.T) {
	for _, pair := range []string{"sha1=abc", "read=3", "crc=1"} {
		_, err := ParseExpected([]string{pair})
		if err == nil || !strings.Contains(err.Error(), "unknown key") || !strings.Contains(err.Error(), "bases") {
			t.Errorf("%q: %v", pair, err)
		}
	}
	for _, pair := range []string{"md5", "md5=", "=abc"} {
		if _, err := ParseExpected([]string{pair}); err == nil || !strings.Contains(err.Error(), "want key=value") && !strings.Contains(err.Error(), "unknown key") {
			t.Errorf("%q: %v", pair, err)
		}
	}
}
