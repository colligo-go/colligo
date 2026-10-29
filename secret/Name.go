package secret

import (
	"encoding/json"

	"github.com/colligo-go/colligo/internal/fsaccess"
)

// Name wraps a file name or path. Classification travels inside the value:
// only code that knows it can construct a restricted Name.
type Name struct {
	raw        string
	opaque     string
	restricted bool
}

func NewName(raw string) Name { return Name{raw: raw} }

func (n Name) String() string {
	if n.restricted {
		return n.opaque
	}
	return n.raw
}

func (n Name) GoString() string             { return n.String() }
func (n Name) MarshalJSON() ([]byte, error) { return json.Marshal(n.String()) }

// Raw returns the real path, public or restricted. It requires an
// fsaccess.FSAccess, whose constructor is private to internal/fsaccess —
// logging, export or manifest code can't obtain one, so a call to Raw from
// there fails to compile.
func (n Name) Raw(fs fsaccess.FSAccess) string {
	_ = fs
	return n.raw
}
