package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrossCheckThatCannotBeWrittenFailsLoudWithExit2(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"colligo.sha256 is a directory": func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "colligo.sha256"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"colligo.b3 is a directory": func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "colligo.b3"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
	}
	if os.Geteuid() != 0 {
		cases["the folder is not writable"] = func(t *testing.T, dir string) {
			if err := os.Chmod(dir, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(dir, 0o755) })
		}
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			data := t.TempDir()
			p := put(t, data, "s.fastq", fastq("S", 30))
			work := t.TempDir()
			block(t, work)
			t.Chdir(work)
			code, out, errText := invoke("check", p, "--cross-check")
			if code != 2 {
				t.Fatalf("exit %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errText)
			}
			if !strings.HasPrefix(errText, "colligo: ") || !strings.Contains(errText, "colligo.") {
				t.Errorf("stderr %q does not say what failed", errText)
			}
		})
	}
}
