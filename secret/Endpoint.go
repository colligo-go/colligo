package secret

import (
	"encoding/json"
	"net/url"
)

// Endpoint wraps a URL. The query string never appears in String, GoString
// or MarshalJSON, public or restricted, because that's where credentials hide.
type Endpoint struct {
	u          *url.URL
	restricted bool
}

func NewEndpoint(raw string) (Endpoint, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Endpoint{}, err
	}
	return Endpoint{u: u}, nil
}

func (e Endpoint) String() string {
	if e.u == nil {
		return ""
	}
	if e.restricted {
		return e.u.Scheme + "://" + e.u.Host + "/[REDACTED]"
	}
	redacted := *e.u
	redacted.User = nil
	redacted.RawQuery = ""
	redacted.Fragment = ""
	return redacted.String()
}

func (e Endpoint) GoString() string             { return e.String() }
func (e Endpoint) MarshalJSON() ([]byte, error) { return json.Marshal(e.String()) }
