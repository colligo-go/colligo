package request

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/colligo-go/colligo/internal/fsaccess"
	"github.com/colligo-go/colligo/secret"
)

const maxListLine = 1 << 20

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// FileList resolves a text file with one path per line. Blank lines and lines
// that start with # are skipped, and a relative path is relative to the
// folder that holds the list. A line whose file does not exist is not an
// error here: the check reports it as unreadable, naming the line, and the
// other files are still checked. A line that names a folder is a usage error,
// because whether to descend into it is exactly the guess Colligo does not make.
type FileList struct {
	Name   secret.Name
	reader io.Reader
}

// ListFromStdin returns the resolver of a list read from r, usually standard
// input. Its paths are relative to the working directory.
func ListFromStdin(r io.Reader) FileList { return FileList{reader: r} }

func (l FileList) Resolve(ctx context.Context) (Found, error) {
	fromStdin := l.reader != nil
	reader := l.reader
	if !fromStdin {
		f, err := fsaccess.Open(l.Name.Raw)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return Found{}, usage("--files-from: the list %s does not exist", l.Name)
			}
			return Found{}, err
		}
		defer f.Close()
		reader = f
	}
	br := bufio.NewReader(reader)
	if head, err := br.Peek(len(utf8BOM)); err == nil && bytes.Equal(head, utf8BOM) {
		if _, err := br.Discard(len(utf8BOM)); err != nil {
			return Found{}, err
		}
	}
	scanner := bufio.NewScanner(br)
	scanner.Buffer(make([]byte, 0, 64<<10), maxListLine)
	var out Found
	for line := 1; scanner.Scan(); line++ {
		if err := ctx.Err(); err != nil {
			return Found{}, err
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if strings.ContainsRune(text, 0) {
			return Found{}, usage("--files-from: line %d has binary content; a list holds one path per line, not a data file", line)
		}
		name, err := l.nameOf(text, fromStdin)
		if err != nil {
			return Found{}, usage("--files-from: line %d: %v", line, err)
		}
		if isDir, err := fsaccess.IsDir(name.Raw); err == nil && isDir {
			return Found{}, usage("--files-from: line %d names a folder; list its files, or give the folder as an argument", line)
		}
		out.Files = addFile(out.Files, File{Name: name, Origins: []Origin{{Kind: FromListLine, Line: line}}})
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return Found{}, usage("--files-from: a line is longer than %d bytes; this is not a list of paths", maxListLine)
		}
		return Found{}, err
	}
	if len(out.Files) == 0 {
		return Found{}, usage("--files-from: the list names no file (blank lines and lines that start with # are skipped)")
	}
	return out, nil
}

func (l FileList) nameOf(text string, fromStdin bool) (secret.Name, error) {
	name, err := NameFromArgument(text)
	if err != nil {
		return secret.Name{}, err
	}
	if strings.HasPrefix(strings.ToLower(text), "file://") {
		return name, nil
	}
	if filepath.IsAbs(text) {
		return secret.NewName(filepath.Clean(text)), nil
	}
	if fromStdin {
		return secret.NewName(filepath.Clean(text)), nil
	}
	return secret.NewName(fsaccess.Resolve(l.Name.Raw, text)), nil
}
