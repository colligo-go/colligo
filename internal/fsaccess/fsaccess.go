// Package fsaccess issues the capability that authorizes secret.Name.Raw
// to return a real path, and performs the filesystem operations that need
// one. No other package can construct an FSAccess: the only constructor is
// private to this package. Errors never carry the path.
package fsaccess

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type FSAccess struct{}

func newFSAccess() FSAccess { return FSAccess{} }

// Reveal calls fn with a freshly built FSAccess. fn is normally the method
// value of secret.Name.Raw, e.g. fsaccess.Reveal(name.Raw).
func Reveal(fn func(FSAccess) string) string { return fn(newFSAccess()) }

// Entry is one directory entry. A symbolic link to a directory has DirLink
// set and Dir clear, so a walk never follows it.
type Entry struct {
	Path    string
	Dir     bool
	DirLink bool
}

func Open(name func(FSAccess) string) (*os.File, error) {
	f, err := os.Open(Reveal(name))
	return f, sanitize(err)
}

func Create(name func(FSAccess) string, suffix string) (*os.File, error) {
	f, err := os.Create(Reveal(name) + suffix)
	return f, sanitize(err)
}

func IsDir(name func(FSAccess) string) (bool, error) {
	info, err := os.Stat(Reveal(name))
	if err != nil {
		return false, sanitize(err)
	}
	return info.IsDir(), nil
}

func ReadDir(name func(FSAccess) string) ([]Entry, error) {
	root := Reveal(name)
	info, err := os.Stat(root)
	if err != nil {
		return nil, sanitize(err)
	}
	if !info.IsDir() {
		return []Entry{{Path: root}}, nil
	}
	items, err := os.ReadDir(root)
	if err != nil {
		return nil, sanitize(err)
	}
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		path := filepath.Join(root, it.Name())
		entry := Entry{Path: path, Dir: it.IsDir()}
		if it.Type()&os.ModeSymlink != 0 {
			target, err := os.Stat(path)
			entry.DirLink = err == nil && target.IsDir()
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func sanitize(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("%s: %w", pe.Op, pe.Err)
	}
	return err
}
