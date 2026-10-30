package verifier

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/secret"
)

func interleavedFastq(pairs int) []byte {
	var b bytes.Buffer
	for i := 1; i <= pairs; i++ {
		for _, mate := range []int{1, 2} {
			fmt.Fprintf(&b, "@SRR9.%d/%d\nACGTACGT\n+\nFFFFFFFF\n", i, mate)
		}
	}
	return b.Bytes()
}

func TestInterleavedFastqPassesAsASingleFileWithoutPairClaims(t *testing.T) {
	cases := map[string][]string{
		"alone":                       {"inter.fastq"},
		"alone with a mate-like name": {"inter_R1.fastq"},
		"next to an unrelated file":   {"inter.fastq", "other.fastq"},
	}
	for label, bases := range cases {
		dir := t.TempDir()
		names := make([]secret.Name, len(bases))
		for i, base := range bases {
			names[i] = write(t, dir, base, interleavedFastq(50))
		}
		rep := run(t, Options{}, names...)
		file := rep.Files[0]
		if file.Status != StatusVerified || len(failed(file)) != 0 {
			t.Errorf("%s: status %s, failed %v", label, file.Status, failed(file))
		}
		for _, a := range file.Assertions {
			if a.ID == diagnoses.PairCount || a.ID == diagnoses.PairIDs || a.ID == diagnoses.PairMissing {
				t.Errorf("%s: pair assertion %s on an interleaved file", label, a.ID)
			}
		}
	}
}
