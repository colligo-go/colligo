package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/verifier"
)

func TestExitCodeTable(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"none":     put(t, dir, "clean.fastq", fastq("S", 20)),
		"advisory": put(t, dir, "genome.fasta", fastq("S", 20)),
		"blocking": put(t, dir, "mystery.dat", []byte{0, 1, 2, 3}),
		"fatal":    put(t, dir, "page.gtf.gz", []byte("<!DOCTYPE html><title>Error</title>")),
	}
	severityCode := func(severity string, permissive bool) int {
		switch severity {
		case "fatal":
			return verifier.ExitFatal
		case "blocking":
			if permissive {
				return verifier.ExitOK
			}
			return verifier.ExitIncomplete
		}
		return verifier.ExitOK
	}
	for severity, path := range files {
		for _, command := range []string{"check", "validate"} {
			for _, quick := range []bool{false, true} {
				for _, permissive := range []bool{false, true} {
					args := []string{command, path}
					if quick {
						args = append(args, "--quick")
					}
					if permissive {
						args = append(args, "--permissive")
					}
					want := severityCode(severity, permissive)
					if command == "validate" && quick {
						want = verifier.ExitUsage
					}
					label := fmt.Sprintf("%s severity=%s quick=%v permissive=%v", command, severity, quick, permissive)
					if code, out, _ := invoke(args...); code != want {
						t.Errorf("%s: exit %d, want %d\n%s", label, code, want, out)
					}
				}
			}
		}
	}
}

func TestRepeatedExpectedKeyIsAUsageErrorBeforeAnyFileIsRead(t *testing.T) {
	missing := t.TempDir() + "/never-read.fastq"
	for _, args := range [][]string{
		{"check", "--expected", "md5=aa", "--expected", "md5=bb", missing},
		{"check", "--expected", "md5=aa", "--expected", "MD5=aa", missing},
		{"check", "--expected", "reads=5", "--expected", "reads=5", missing},
	} {
		code, out, errText := invoke(args...)
		if code != verifier.ExitUsage || out != "" {
			t.Errorf("%v: exit %d out %q", args, code, out)
		}
		if !strings.Contains(errText, "given more than once") || strings.Contains(errText, "never-read") {
			t.Errorf("%v: stderr %q", args, errText)
		}
	}
}
