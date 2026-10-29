package format

import (
	"strings"
	"testing"
)

func fuzzValidator(f *testing.F, v FormatValidator, seeds ...string) {
	for _, s := range seeds {
		f.Add([]byte(s), uint8(7))
	}
	f.Fuzz(func(t *testing.T, data []byte, chunk uint8) {
		e := v.NewExtractor()
		step := int(chunk) + 1
		for len(data) > 0 {
			n := min(step, len(data))
			if _, err := e.Write(data[:n]); err != nil {
				t.Fatal(err)
			}
			data = data[n:]
		}
		facts, err := e.Finish()
		if err != nil {
			t.Fatal(err)
		}
		if len(facts) == 0 || facts[0].Kind != "structure" {
			t.Fatalf("the first fact must be structure: %+v", facts)
		}
		for _, fact := range facts {
			if !strings.HasPrefix(fact.ExtractorVersion, v.Format()) {
				t.Fatalf("fact version %q does not belong to %s", fact.ExtractorVersion, v.Format())
			}
		}
	})
}

func FuzzValidateFastq(f *testing.F) {
	fuzzValidator(f, fastqValidator{}, "@r/1 x\nACGT\n+\nIIII\n", "@r\nAC\n+\nI\n", "@", "@r\nACGT\n+\nII")
}

func FuzzValidateFasta(f *testing.F) {
	fuzzValidator(f, fastaValidator{}, ">chr1 d\nACGT\n>chr2\nGG\n", ">", ">a\n\n\n")
}

func FuzzValidateGTF(f *testing.F) {
	fuzzValidator(f, gtfValidator{}, "#h\n1\ta\tb\t1\t2\t.\t+\t.\tx\n", "1\ta\n", "\n\n")
}
