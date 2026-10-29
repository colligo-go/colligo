package checksum

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"lukechampine.com/blake3"

	"github.com/colligo-go/colligo/model"
)

type vector struct {
	algorithm string
	input     []byte
	want      string
}

var vectors = []vector{
	{"BLAKE3", nil, "af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262"},
	{"BLAKE3", []byte{0x00}, "2d3adedff11b61f14c886e35afa036736dcd87a74d27b5c1510225d0f592e213"},
	{"MD5", nil, "d41d8cd98f00b204e9800998ecf8427e"},
	{"MD5", []byte{0x61, 0x62, 0x63}, "900150983cd24fb0d6963f7d28e17f72"},
	{"SHA-256", nil, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	{"SHA-256", []byte{0x61, 0x62, 0x63}, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
}

type SelfTestError struct {
	Algorithm string
	Want      string
	Got       string
}

func (e *SelfTestError) Error() string {
	return fmt.Sprintf("checksum: %s vector mismatch: want %s, got %s", e.Algorithm, e.Want, e.Got)
}

// Severity is always fatal: a hash failing its own known vector can't be
// trusted for any other decision.
func (e *SelfTestError) Severity() model.Severity { return model.Fatal }

func sum(algorithm string, input []byte) (string, error) {
	switch algorithm {
	case "BLAKE3":
		h := blake3.Sum256(input)
		return hex.EncodeToString(h[:]), nil
	case "MD5":
		h := md5.Sum(input)
		return hex.EncodeToString(h[:]), nil
	case "SHA-256":
		h := sha256.Sum256(input)
		return hex.EncodeToString(h[:]), nil
	default:
		return "", fmt.Errorf("checksum: unknown algorithm %q", algorithm)
	}
}

// Verify must be called explicitly at startup, never from init(), which
// would hide the side effect.
func Verify() error {
	for _, v := range vectors {
		got, err := sum(v.algorithm, v.input)
		if err != nil {
			return err
		}
		if got != v.want {
			return &SelfTestError{Algorithm: v.algorithm, Want: v.want, Got: got}
		}
	}
	return nil
}
