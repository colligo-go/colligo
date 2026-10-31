package secret

import "net/url"

// Join returns a copy of e with elem appended to the path and query as the
// query string, so a caller builds a request URL without ever holding it as
// a string. The copy keeps the restriction of e.
func (e Endpoint) Join(query url.Values, elem ...string) Endpoint {
	if e.u == nil {
		return Endpoint{}
	}
	joined := e.u.JoinPath(elem...)
	joined.RawQuery = query.Encode()
	joined.Fragment = ""
	return Endpoint{u: joined, restricted: e.restricted}
}
