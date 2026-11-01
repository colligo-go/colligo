package verifier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/model"
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
		diagnoses.ChangedDuringRead: func() Report {
			rep, _ := checkWhileChanging(t, reads(50_000), func(path string) {
				if err := os.Truncate(path, 1000); err != nil {
					t.Error(err)
				}
			})
			return Report{Files: []*FileReport{rep}}
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
		diagnoses.InputSRA: func() Report {
			return run(t, Options{}, write(t, dir, "reads.sra", append([]byte("NCBI.sra"), make([]byte, 32)...)))
		},
		diagnoses.InputAlignment: func() Report {
			return run(t, Options{}, write(t, dir, "aligned.bam", bamBytes(t)))
		},
		diagnoses.InputArchive: func() Report {
			return run(t, Options{}, write(t, dir, "bundle.zip", append([]byte("PK\x03\x04"), make([]byte, 32)...)))
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
		diagnoses.PairMissing: func() Report {
			return run(t, Options{},
				write(t, dir, "lonely_R1.fastq", reads(5)),
				write(t, dir, "other.fastq", reads(5)))
		},
		diagnoses.MateCollapsed: func() Report {
			return run(t, Options{},
				write(t, dir, "collapsed_R1.fastq", fastqText("SRR1", 10, strings.Repeat("ACGT", 30))),
				write(t, dir, "collapsed_R2.fastq", fastqText("SRR1", 10, "A")))
		},
		diagnoses.ExtensionMismatch: func() Report {
			return run(t, Options{}, write(t, dir, "genome.fasta", reads(5)))
		},
		diagnoses.TrailingBlankLines: func() Report {
			return run(t, Options{}, write(t, dir, "blank.fastq", append(reads(5), '\n', '\n')))
		},
		diagnoses.SheetRemotePath: func() Report {
			return checkSheet(t, dir, "sample,fastq_1,fastq_2\nR,s3://bucket/r.fq.gz,\n")
		},
		diagnoses.SheetSharedFile: func() Report {
			writeMates(t, dir, "shared_a", "SHA", 5)
			writeMates(t, dir, "shared_b", "SHB", 5)
			return checkSheet(t, dir, "sample,fastq_1,fastq_2\nA,shared_a_R1.fastq,shared_a_R2.fastq\nB,shared_b_R1.fastq,shared_a_R2.fastq\n")
		},
		diagnoses.SheetDuplicateRow: func() Report {
			writeMates(t, dir, "dup_a", "DUA", 5)
			return checkSheet(t, dir, "sample,fastq_1,fastq_2\nA,dup_a_R1.fastq,dup_a_R2.fastq\nA,dup_a_R1.fastq,dup_a_R2.fastq\n")
		},
		diagnoses.SheetMatesSwapped: func() Report {
			writeMates(t, dir, "swap_a", "SWA", 5)
			writeMates(t, dir, "swap_b", "SWB", 5)
			return checkSheet(t, dir, "sample,fastq_1,fastq_2\nA,swap_a_R1.fastq,swap_b_R2.fastq\nB,swap_b_R1.fastq,swap_a_R2.fastq\n")
		},
		diagnoses.SourceStatus: func() Report {
			return against(t, Declaration{Samples: []model.Sample{sample("ERR1", "PAIRED", "suppressed", nil, nil)}})
		},
		diagnoses.SourceFileMissing: func() Report {
			return against(t, Declaration{Samples: []model.Sample{sample("ERR2", "SINGLE", "public", nil, nil, "ERR2.fastq.gz")}})
		},
		diagnoses.SourceFileUnlisted: func() Report {
			return against(t, Declaration{Samples: []model.Sample{sample("ERR3", "SINGLE", "public", nil, nil)}},
				write(t, dir, "stray.fastq", reads(5)))
		},
		diagnoses.SourceFileAmbiguous: func() Report {
			return against(t, Declaration{Samples: []model.Sample{sample("ERR4", "SINGLE", "public", nil, nil, "ERR4.fastq.gz")}},
				write(t, mkdir(t, filepath.Join(dir, "copy_a")), "ERR4.fastq", reads(5)),
				write(t, mkdir(t, filepath.Join(dir, "copy_b")), "ERR4.fastq", reads(5)))
		},
		diagnoses.ReadCountUnexplained: func() Report {
			return against(t, Declaration{Samples: []model.Sample{sample("ERR5", "SINGLE", "public", count(6), count(40), "ERR5.fastq.gz")}},
				write(t, dir, "ERR5.fastq", reads(5)))
		},
		diagnoses.LayoutMismatch: func() Report {
			return against(t, Declaration{Samples: []model.Sample{sample("ERR6", "SINGLE", "public", count(10), count(80), "ERR6_1.fastq.gz", "ERR6_2.fastq.gz")}},
				write(t, dir, "ERR6_1.fastq", reads(5)),
				write(t, dir, "ERR6_2.fastq", reads(5)))
		},
		diagnoses.SourceRunFailed: func() Report {
			return against(t, Declaration{Samples: []model.Sample{sample("ERR9", "SINGLE", "public", nil, count(1), "ERR9.fastq.gz")}},
				write(t, dir, "ERR9.fastq", reads(5)))
		},
		diagnoses.SourceRowRejected: func() Report {
			return against(t, Declaration{Issues: []model.ValidationIssue{{Severity: model.Blocking, Code: "bad_md5", Subject: "ERR7", Message: "fastq_md5 is malformed"}}})
		},
		diagnoses.SourceFieldEmpty: func() Report {
			return against(t, Declaration{Issues: []model.ValidationIssue{{Severity: model.Advisory, Code: "empty_field", Subject: "ERR8", Message: "read_count is empty"}}})
		},
	}

	fired := map[string]bool{}
	for id, build := range scenarios {
		hit := false
		rep := build()
		groups := [][]model.Assertion{}
		for _, f := range rep.Files {
			groups = append(groups, f.Assertions)
		}
		if rep.Source != nil {
			groups = append(groups, rep.Source.Assertions)
			for _, r := range rep.Source.Runs {
				groups = append(groups, r.Assertions)
			}
		}
		for _, as := range groups {
			for _, a := range as {
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
