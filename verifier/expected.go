package verifier

import (
	"fmt"
	"strconv"
	"strings"
)

func ParseExpected(pairs []string) (Expected, error) {
	var exp Expected
	seen := map[string]string{}
	for _, p := range pairs {
		key, value, ok := strings.Cut(p, "=")
		if !ok || value == "" {
			return Expected{}, fmt.Errorf("--expected %q: want key=value", p)
		}
		key = strings.ToLower(key)
		if previous, dup := seen[key]; dup {
			return Expected{}, fmt.Errorf("--expected %s given more than once (%q and %q)", key, previous, value)
		}
		seen[key] = value
		switch key {
		case "md5", "sha256", "blake3":
			want := hexLengths[key]
			if len(value) != want || !isHex(value) {
				return Expected{}, fmt.Errorf("--expected %q: %s is %d hexadecimal characters", p, key, want)
			}
			switch key {
			case "md5":
				exp.MD5 = value
			case "sha256":
				exp.SHA256 = value
			default:
				exp.BLAKE3 = value
			}
		case "size", "reads", "bases":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 {
				return Expected{}, fmt.Errorf("--expected %q: %q is not a count", p, value)
			}
			switch key {
			case "size":
				exp.Size = &n
			case "reads":
				exp.Reads = &n
			default:
				exp.Bases = &n
			}
		default:
			return Expected{}, fmt.Errorf("--expected %q: unknown key (use md5, sha256, blake3, size, reads, bases)", p)
		}
	}
	return exp, nil
}

var hexLengths = map[string]int{"md5": 32, "sha256": 64, "blake3": 64}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
