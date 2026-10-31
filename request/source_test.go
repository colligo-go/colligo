package request_test

import (
	"strings"
	"testing"

	"github.com/colligo-go/colligo/request"
	"github.com/colligo-go/colligo/secret"
)

func filesNamed(bases ...string) []request.File {
	out := make([]request.File, len(bases))
	for i, b := range bases {
		out[i] = request.File{Name: secret.NewName("reads/" + b)}
	}
	return out
}

func TestParseSourceAcceptsWhatPeopleType(t *testing.T) {
	cases := []struct {
		value     string
		accession string
		from      request.Provenance
	}{
		{"ena:DRR021374", "DRR021374", request.Typed},
		{"ena:drr021374", "DRR021374", request.Normalized},
		{"  ENA : drr021374  ", "DRR021374", request.Normalized},
		{"ENA:PRJDB0001", "PRJDB0001", request.Typed},
		{"https://www.ebi.ac.uk/ena/browser/view/DRR021374", "DRR021374", request.FromLink},
		{"https://www.ebi.ac.uk/ena/browser/view/drr021374?show=reads", "DRR021374", request.FromLink},
		{"http://www.ebi.ac.uk/ena/data/view/DRR021374&display=xml", "DRR021374", request.FromLink},
		{"https://www.ebi.ac.uk/ena/browser/view/PRJDB0001", "PRJDB0001", request.FromLink},
		{"www.ebi.ac.uk/ena/browser/view/ERR12477684", "ERR12477684", request.FromLink},
		{"  https://ebi.ac.uk/ena/browser/view/SAMD00000001  ", "SAMD00000001", request.FromLink},
	}
	for _, c := range cases {
		ref, err := request.ParseSource(c.value, nil)
		if err != nil {
			t.Errorf("%q: %v", c.value, err)
			continue
		}
		if ref.ID != "ena" || ref.Accession != c.accession || ref.Provenance != c.from {
			t.Errorf("%q: got %+v, want accession %s from %q", c.value, ref, c.accession, c.from)
		}
	}
}

func TestParseSourceRefusesWhatItCannotReadWithAMessageThatHelps(t *testing.T) {
	cases := map[string]string{
		"":         "--source takes",
		"geo:GSE1": "only source today is ena",
		"ena:":     "needs an accession",
		"https://www.ncbi.nlm.nih.gov/sra/SRR10663037":                 "not an ENA address",
		"https://www.ebi.ac.uk/ebisearch/search?db=x":                  "not a page of the ENA",
		"https://www.ebi.ac.uk/ena/browser/home":                       "no accession found",
		"https://www.ebi.ac.uk/ena/browser/view/DRR021374/ERR12477684": "more than one accession",
		"https://": "not valid",
	}
	for value, want := range cases {
		_, err := request.ParseSource(value, nil)
		if err == nil {
			t.Errorf("%q was accepted", value)
			continue
		}
		if msg := usageMessage(t, err); !strings.Contains(msg, want) {
			t.Errorf("%q: message %q lacks %q", value, msg, want)
		}
	}
}

func TestEnaAloneDeducesTheAccessionOfOneRun(t *testing.T) {
	ref, err := request.ParseSource("ena", filesNamed("DRR021374_1.fastq.gz", "DRR021374_2.fastq.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if ref.Accession != "DRR021374" || ref.Provenance != request.Deduced {
		t.Errorf("got %+v", ref)
	}
	if ref, err = request.ParseSource(" ENA ", filesNamed("ERR12477684.fastq.gz")); err != nil || ref.Accession != "ERR12477684" {
		t.Errorf("single-end: %+v %v", ref, err)
	}
}

func TestEnaAloneNeverPicksBetweenRuns(t *testing.T) {
	_, err := request.ParseSource("ena", filesNamed("DRR021374_1.fastq.gz", "ERR12477684_1.fastq.gz", "DRR021374_2.fastq.gz"))
	msg := usageMessage(t, err)
	for _, want := range []string{"DRR021374", "ERR12477684", "2 runs", "study"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

func TestEnaAloneDoesNotDeduceFromFilesThatCarryNoRun(t *testing.T) {
	cases := map[string][]request.File{
		"one file without an accession":   filesNamed("DRR021374_1.fastq.gz", "notes.fastq"),
		"a study is never deduced":        filesNamed("PRJDB0001_1.fastq.gz"),
		"an accession glued to digits":    filesNamed("DRR021374x.fastq.gz"),
		"no files at all":                 nil,
		"lowercase names are not guessed": filesNamed("drr021374_1.fastq.gz"),
	}
	for name, files := range cases {
		if _, err := request.ParseSource("ena", files); err == nil {
			t.Errorf("%s: an accession was deduced", name)
		} else {
			usageMessage(t, err)
		}
	}
}

func TestDeducingFromNamesWithNoRunDescribesThemByCount(t *testing.T) {
	cases := map[string]struct {
		files []request.File
		want  string
	}{
		"one":   {filesNamed("a.fastq"), "because a.fastq carries no run accession"},
		"three": {filesNamed("a.fastq", "b.fastq", "c.fastq"), "because 3 files (a.fastq, b.fastq, c.fastq) carry no run accession"},
		"five":  {filesNamed("a.fastq", "b.fastq", "c.fastq", "d.fastq", "e.fastq"), "because 5 files (a.fastq, b.fastq, c.fastq, ...) carry no run accession"},
	}
	for name, tc := range cases {
		_, err := request.ParseSource("ena", tc.files)
		msg := usageMessage(t, err)
		if !strings.Contains(msg, tc.want) {
			t.Errorf("%s: message %q lacks %q", name, msg, tc.want)
		}
		if name == "five" && (strings.Contains(msg, "d.fastq") || strings.Contains(msg, "e.fastq")) {
			t.Errorf("%s: message lists more than three names: %q", name, msg)
		}
	}
}
