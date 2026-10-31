package identity

import (
	"errors"
	"strconv"
	"unicode/utf8"
)

var errInvalidUTF8 = errors.New("a sequence name is not valid UTF-8")

const hexDigits = "0123456789abcdef"

func appendJSONString(dst []byte, s string) ([]byte, error) {
	if !utf8.ValidString(s) {
		return nil, errInvalidUTF8
	}
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			dst = append(dst, '\\', '"')
		case c == '\\':
			dst = append(dst, '\\', '\\')
		case c == '\b':
			dst = append(dst, '\\', 'b')
		case c == '\f':
			dst = append(dst, '\\', 'f')
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		default:
			dst = append(dst, c)
		}
	}
	return append(dst, '"'), nil
}

func canonicalStrings(items []string) ([]byte, error) {
	out := []byte{'['}
	for i, item := range items {
		if i > 0 {
			out = append(out, ',')
		}
		var err error
		if out, err = appendJSONString(out, item); err != nil {
			return nil, err
		}
	}
	return append(out, ']'), nil
}

func canonicalIntegers(items []int64) []byte {
	out := []byte{'['}
	for i, item := range items {
		if i > 0 {
			out = append(out, ',')
		}
		out = strconv.AppendInt(out, item, 10)
	}
	return append(out, ']')
}
