package request

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/colligo-go/colligo/secret"
)

// NameFromArgument turns a path as the person typed it into a secret.Name. A
// file:// address, as a file manager pastes it, is read as the path it names.
func NameFromArgument(raw string) (secret.Name, error) {
	if !strings.HasPrefix(strings.ToLower(raw), "file://") {
		return secret.NewName(raw), nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return secret.Name{}, usage("the file:// address is not valid")
	}
	if host := strings.ToLower(u.Host); host != "" && host != "localhost" {
		return secret.Name{}, usage("the file:// address names another computer (%s); only files on this one can be read", u.Host)
	}
	if u.Path == "" || u.RawQuery != "" || u.Fragment != "" {
		return secret.Name{}, usage("the file:// address must be file:///path/to/file, with no query or fragment")
	}
	return secret.NewName(filepath.Clean(u.Path)), nil
}
