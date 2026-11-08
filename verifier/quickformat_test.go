package verifier

import (
	"testing"

	"github.com/colligo-go/colligo/format"
)

func TestQuickUsesTheDeclaredTextFormatButNeverOverAnHTMLPage(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		label, declared, data, want string
	}{
		{"unrecognised start declared fastq", format.ContentFastq, "ACGT\n+\nIIII\n", format.ContentFastq},
		{"HTML page declared fastq", format.ContentFastq, "<!DOCTYPE html><title>Error</title>\n", format.ContentHTML},
		{"unrecognised start declared gzip", format.ContainerGzip, "ACGT\n+\nIIII\n", format.ContentUnknown},
	}
	for _, tc := range cases {
		rep := run(t, Options{Quick: true, Format: tc.declared}, write(t, dir, "sample.dat", []byte(tc.data))).Files[0]
		if rep.Content != tc.want {
			t.Errorf("%s: content %q, want %q", tc.label, rep.Content, tc.want)
		}
		if rep.Status == StatusVerified {
			t.Errorf("%s: --quick produced a verified file", tc.label)
		}
	}
}
