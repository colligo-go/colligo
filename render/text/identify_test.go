package text_test

import (
	"strings"
	"testing"

	"github.com/colligo-go/colligo/identity"
	"github.com/colligo-go/colligo/render/text"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/verifier"
)

func identification(base, status string) *verifier.Identification {
	return &verifier.Identification{Name: secret.NewName("/data/" + base), Status: status, Content: "gtf", Size: 100}
}

func identified(t *testing.T, code int, files ...*verifier.Identification) string {
	t.Helper()
	var out strings.Builder
	if err := text.Identify(&out, verifier.IdentifyReport{Files: files}, options(code)); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestIdentifyVerdictSaysWhatIsAndIsNotConfirmed(t *testing.T) {
	gencode := &identity.Gencode{Label: "GENCODE 44", Release: "44", Organism: "human"}
	declared := identification("a.gtf", verifier.IdentityDeclared)
	declared.Gencode = gencode
	conflict := identification("b.gtf", verifier.IdentityConflict)
	conflict.Gencode, conflict.Reason = gencode, "the header says GENCODE 44, but the names are Ensembl's"
	known := identification("c.fa", verifier.IdentityIdentified)
	known.Reference = &identity.Reference{Label: "GRCh38", Source: "ref"}
	known.Seqcol = &identity.SeqcolValue{Digest: "abc", Sequences: 3}
	cases := []struct {
		name  string
		files []*verifier.Identification
		code  int
		first string
	}{
		{"identified", []*verifier.Identification{known}, 0, "✓ IDENTIFIED  c.fa is GRCh38  exit 0"},
		{"declared", []*verifier.Identification{declared}, 0, "! NOT CONFIRMED  a.gtf says it is GENCODE 44, and nothing official confirms it  exit 0"},
		{"conflict", []*verifier.Identification{conflict}, 3, "✗ FAILED  b.gtf says it is GENCODE 44, but its content says otherwise  exit 3"},
		{"unreadable", []*verifier.Identification{identification("d.gtf", verifier.StatusUnreadable)}, 2, "✗ FAILED  1 file could not be read: nothing is identified for them  exit 2"},
		{"several", []*verifier.Identification{known, declared, conflict}, 3, "✗ FAILED  3 files: 1 identified, 1 declared, 1 conflict  exit 3"},
		{"none", nil, 3, "✗ NOTHING IDENTIFIED  no file was found, so nothing is identified  exit 3"},
	}
	for _, c := range cases {
		out := identified(t, c.code, c.files...)
		if first := strings.SplitN(out, "\n", 2)[0]; first != c.first {
			t.Errorf("%s: first line %q, want %q", c.name, first, c.first)
		}
	}
}

func TestIdentifyNeverCallsADeclaredHeaderConfirmed(t *testing.T) {
	declared := identification("a.gtf", verifier.IdentityDeclared)
	declared.Gencode = &identity.Gencode{Label: "GENCODE 44", Release: "44", Organism: "human"}
	out := identified(t, 0, declared)
	if strings.Contains(out, "✓ IDENTIFIED") || !strings.Contains(out, "only a matching official checksum proves it") {
		t.Errorf("a declared file must stay unconfirmed:\n%s", out)
	}
}

func TestIdentifyQuietPrintsOnlyTheVerdict(t *testing.T) {
	var out strings.Builder
	o := options(0)
	o.Quiet = true
	known := identification("c.fa", verifier.IdentityNotIdentified)
	if err := text.Identify(&out, verifier.IdentifyReport{Files: []*verifier.Identification{known}}, o); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Errorf("%q", out.String())
	}
}
