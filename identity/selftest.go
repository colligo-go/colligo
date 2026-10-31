package identity

import (
	"fmt"

	"github.com/colligo-go/colligo/model"
)

type vector struct {
	input string
	want  string
}

var vectors = []vector{
	{"", "z4PhNX7vuL3xVChQ1m2AB9Yg5AULVxXc"},
	{"ACGT", "aKF498dAxcJAqme6QYQ7EZ07-fiw8Kw2"},
}

// SelfTestError means the digest of this build disagrees with the published
// refget vectors, so no identity it computes can be trusted.
type SelfTestError struct {
	Input string
	Want  string
	Got   string
}

func (e *SelfTestError) Error() string {
	return fmt.Sprintf("identity: sha512t24u vector mismatch for %q: want %s, got %s", e.Input, e.Want, e.Got)
}

func (e *SelfTestError) Severity() model.Severity { return model.Fatal }

// Verify must run before any digest is trusted. It also rejects an embedded
// checksum table with an entry that has no origin or date. Call it explicitly at
// startup, never from init().
func Verify() error {
	for _, v := range vectors {
		if got := Sha512t24u([]byte(v.input)); got != v.want {
			return &SelfTestError{Input: v.input, Want: v.want, Got: got}
		}
	}
	_, err := KnownChecksums()
	return err
}
