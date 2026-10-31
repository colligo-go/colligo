// Package identity names a file by what it contains. It is pure: it never
// opens a file and never touches a database. The bytes arrive through Write,
// from the single read of the file.
package identity

import (
	"crypto/sha512"
	"encoding/base64"
)

const digestBytes = 24

// Sha512t24u is the GA4GH truncated digest: SHA-512, first 24 bytes,
// base64url without padding. The result always has 32 characters.
func Sha512t24u(data []byte) string {
	sum := sha512.Sum512(data)
	return base64.RawURLEncoding.EncodeToString(sum[:digestBytes])
}
