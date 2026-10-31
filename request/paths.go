package request

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/internal/fsaccess"
	"github.com/colligo-go/colligo/secret"
)

const headBytes = 64 << 10

// Paths resolves files and folders given as arguments. A folder is read
// whole; Recursive descends into its subfolders.
type Paths struct {
	Names     []secret.Name
	Recursive bool
}

func (p Paths) Resolve(ctx context.Context) (Found, error) {
	var out Found
	for _, name := range p.Names {
		isDir, err := fsaccess.IsDir(name.Raw)
		if err != nil {
			return Found{}, fmt.Errorf("%s: %w", name, err)
		}
		found, skipped, links, err := Discover(ctx, name, p.Recursive)
		if err != nil {
			return Found{}, fmt.Errorf("%s: %w", name, err)
		}
		kind := FromArgument
		if isDir {
			kind = FromFolder
		}
		for _, f := range found {
			out.Files = append(out.Files, File{Name: f, Origins: []Origin{{Kind: kind}}})
		}
		out.Skipped = append(out.Skipped, skipped...)
		out.SkippedLinks = append(out.SkippedLinks, links...)
	}
	return out, nil
}

// Discover classifies the files of a directory by content. A file is a
// candidate when its content is a known format or its name says it should
// be one, so a web page saved as .gtf.gz is rejected instead of ignored. A
// file named explicitly is always checked.
func Discover(ctx context.Context, root secret.Name, recursive bool) (names []secret.Name, skipped, dirLinks []string, err error) {
	isDir, err := fsaccess.IsDir(root.Raw)
	if err != nil {
		return nil, nil, nil, err
	}
	if !isDir {
		return []secret.Name{root}, nil, nil, nil
	}
	entries, err := fsaccess.ReadDir(root.Raw)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		name := secret.NewName(e.Path)
		if e.Dir {
			if !recursive || hidden(name) {
				continue
			}
			sub, subSkipped, subLinks, err := Discover(ctx, name, recursive)
			if err != nil {
				return nil, nil, nil, err
			}
			names, skipped, dirLinks = append(names, sub...), append(skipped, subSkipped...), append(dirLinks, subLinks...)
			continue
		}
		if e.DirLink {
			if !hidden(name) {
				dirLinks = append(dirLinks, name.String())
			}
			continue
		}
		if hidden(name) {
			continue
		}
		if looksLikeData(name) {
			names = append(names, name)
		} else {
			skipped = append(skipped, name.String())
		}
	}
	sort.Strings(skipped)
	sort.Strings(dirLinks)
	return names, skipped, dirLinks, nil
}

func hidden(name secret.Name) bool {
	return strings.HasPrefix(filepath.Base(name.String()), ".")
}

func looksLikeData(name secret.Name) bool {
	f, err := fsaccess.Open(name.Raw)
	if err != nil {
		return true
	}
	defer f.Close()
	head := make([]byte, headBytes)
	n, _ := io.ReadFull(f, head)
	container, content := format.Sniff(head[:n])
	if container != format.ContainerNone || content == format.ContentFastq || content == format.ContentFasta || content == format.ContentGTF {
		return true
	}
	nameContainer, nameContent := format.FromName(name)
	return nameContainer != format.ContainerNone || nameContent != ""
}
