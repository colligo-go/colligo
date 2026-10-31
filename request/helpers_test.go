package request_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/colligo-go/colligo/request"
	"github.com/colligo-go/colligo/secret"
)

func put(t *testing.T, dir, base string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, base)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

var fastqRead = []byte("@r1\nACGT\n+\nFFFF\n")

func bases(files []request.File) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = filepath.Base(f.Name.String())
	}
	return out
}

func names(paths ...string) []secret.Name {
	out := make([]secret.Name, len(paths))
	for i, p := range paths {
		out[i] = secret.NewName(p)
	}
	return out
}

func usageMessage(t *testing.T, err error) string {
	t.Helper()
	var bad *request.UsageError
	if !errorsAs(err, &bad) {
		t.Fatalf("want a *request.UsageError, got %v", err)
	}
	return bad.Message
}
