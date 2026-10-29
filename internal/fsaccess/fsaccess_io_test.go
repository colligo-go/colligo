package fsaccess

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func named(p string) func(FSAccess) string { return func(FSAccess) string { return p } }

func TestOpenErrorNeverCarriesThePath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "patient-123.fastq")
	_, err := Open(named(missing))
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "patient-123") {
		t.Fatalf("the error carries the path: %v", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the cause was lost: %v", err)
	}
}

func TestReadDirIsSortedAndIsDir(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"b", "a", "c"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o644)
	}
	os.Mkdir(filepath.Join(dir, "d"), 0o755)
	entries, err := ReadDir(named(dir))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, filepath.Base(e.Path))
	}
	if strings.Join(got, "") != "abcd" || !entries[3].Dir {
		t.Fatalf("%v", got)
	}
	if isDir, err := IsDir(named(dir)); err != nil || !isDir {
		t.Fatalf("%v %v", isDir, err)
	}
	if isDir, _ := IsDir(named(filepath.Join(dir, "a"))); isDir {
		t.Fatal("a file is not a directory")
	}
}

func TestCreateAppendsSuffix(t *testing.T) {
	base := filepath.Join(t.TempDir(), "x")
	f, err := Create(named(base), ".out")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := os.Stat(base + ".out"); err != nil {
		t.Fatal(err)
	}
}
