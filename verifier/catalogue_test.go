package verifier

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/secret"
)

func TestEveryCatalogueEntryFiresFromAConstructedFile(t *testing.T) {
	dir := t.TempDir()
	reads := func(n int) []byte { return fastqText("SRR1", n, "ACGTACGT") }
	wrongSize := int64(1)
	wrongReads := int64(999)

	scenarios := map[string]func() Report{
		diagnoses.ReadFailed: func() Report {
			return run(t, Options{}, secret.NewName(filepath.Join(dir, "missing.fastq")))
		},
		diagnoses.HTMLBody: func() Report {
			return run(t, Options{}, write(t, dir, "page.gtf.gz", []byte("<!DOCTYPE html><html><body>Login</body></html>")))
		},
		diagnoses.MagicMismatch: func() Report {
			return run(t, Options{}, write(t, dir, "plain.fastq.gz", reads(5)))
		},
		diagnoses.ChecksumMismatch: func() Report {
			return run(t, Options{Expected: Expected{MD5: strings.Repeat("0", 32)}}, write(t, dir, "sum.fastq", reads(5)))
		},
		diagnoses.SizeMismatch: func() Report {
			return run(t, Options{Expected: Expected{Size: &wrongSize}}, write(t, dir, "size.fastq", reads(5)))
		},
		diagnoses.StructureInvalid: func() Report {
			return run(t, Options{}, write(t, dir, "cut.fastq", reads(5)[:40]))
		},
		diagnoses.FormatUnknown: func() Report {
			return run(t, Options{}, write(t, dir, "mystery.dat", []byte{0, 1, 2, 3, 4, 5}))
		},
		diagnoses.FormatDeclared: func() Report {
			return run(t, Options{Format: "bgzf"}, write(t, dir, "declared.fastq.gz", gzipped(t, reads(5))))
		},
		diagnoses.CountMismatch: func() Report {
			return run(t, Options{Expected: Expected{Reads: &wrongReads}}, write(t, dir, "count.fastq", reads(5)))
		},
		diagnoses.PairCount: func() Report {
			return run(t, Options{},
				write(t, dir, "count_R1.fastq", reads(10)),
				write(t, dir, "count_R2.fastq", reads(9)))
		},
		diagnoses.PairIDs: func() Report {
			return run(t, Options{},
				write(t, dir, "ids_R1.fastq", fastqText("AAA", 10, "ACGT")),
				write(t, dir, "ids_R2.fastq", fastqText("BBB", 10, "ACGT")))
		},
		diagnoses.ExtensionMismatch: func() Report {
			return run(t, Options{}, write(t, dir, "genome.fasta", reads(5)))
		},
		diagnoses.TrailingBlankLines: func() Report {
			return run(t, Options{}, write(t, dir, "blank.fastq", append(reads(5), '\n', '\n')))
		},
	}

	fired := map[string]bool{}
	for id, build := range scenarios {
		hit := false
		for _, f := range build().Files {
			for _, a := range f.Assertions {
				if a.ID == id && !a.Passed && !a.Skipped {
					hit = true
				}
			}
		}
		if !hit {
			t.Errorf("%s: its scenario did not produce that failed assertion", id)
		}
		fired[id] = hit
	}
	for _, id := range diagnoses.IDs() {
		if !fired[id] {
			t.Errorf("catalogue entry %s has no scenario that fires it", id)
		}
	}
}
