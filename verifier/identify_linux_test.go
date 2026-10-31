package verifier

import (
	"context"
	"strings"
	"testing"
)

func TestIdentifyFileReadsEveryByteExactlyOnce(t *testing.T) {
	dir := t.TempDir()
	large := []byte(strings.Repeat(">contig\n"+strings.Repeat("ACGTTGCA", 8)+"\n", 20_000))
	files := map[string][]byte{
		"tiny.fa":            []byte(identifyText),
		"large.fa":           large,
		"large.fa.gz":        gzipped(t, large),
		"one-line.fa":        []byte(">a\n" + strings.Repeat("ACGT", 500_000) + "\n"),
		"reads.fastq":        fastqText("SRR1", 20_000, "ACGTACGT"),
		"broken.fa":          []byte(">a\nACGT\n;comment\n"),
		"truncated.fa.gz":    gzipped(t, large)[:4000],
		"empty.fa":           nil,
		"html.fa.gz":         []byte("<!DOCTYPE html><title>Login</title>"),
		"unrecognised.dat":   {0, 1, 2, 3, 4, 5, 6, 7},
		"not-compressed.fgz": large[:4000],
	}
	for base, data := range files {
		name := write(t, dir, base, data)
		got := leastBytesRead(t, int64(len(data)), func() {
			if _, err := IdentifyFile(context.Background(), name, nil); err != nil {
				t.Fatal(err)
			}
		})
		allowed := int64(0)
		if len(data) >= 4*schedulerWakeups {
			allowed = schedulerWakeups
		}
		if got < int64(len(data)) || got > int64(len(data))+allowed {
			t.Errorf("%s: %d bytes read from a file of %d", base, got, len(data))
		}
	}
}
