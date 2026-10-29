package main

import (
	"fmt"
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
