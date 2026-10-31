package verifier

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/identity"
	"github.com/colligo-go/colligo/secret"
)

const identifyText = ">chrA first record\nACGTACGT\nacgtNN\n>chrB\nGGGGCCCC\n\n>chrC desc\nTTTT\n"

const (
	identifyDigest = "-ILNzNQsKYl_9KDXtkCWYUl5t5NESUdA"
	changedDigest  = "N0YRD0n4wtA3-ThKhQSX4fCW0--XU5U0"
)

var tinyReferences = identity.Table{Collections: []identity.Reference{{Label: "tiny", Digest: identifyDigest, Source: "independent Python implementation of the specification"}}}

func identify(t *testing.T, name secret.Name) *Identification {
	t.Helper()
	got, err := IdentifyFile(context.Background(), name, tinyReferences)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestKnownFastaIsIdentifiedAndOneChangedBaseIsNot(t *testing.T) {
	dir := t.TempDir()
	known := identify(t, write(t, dir, "known.fa", []byte(identifyText)))
	if known.Status != IdentityIdentified || known.Reference == nil || known.Reference.Label != "tiny" || known.Seqcol.Digest != identifyDigest {
		t.Errorf("known = %+v", known)
	}
	swapped := identify(t, write(t, dir, "swapped.fa", []byte(strings.Replace(identifyText, "GGGGCCCC", "GGGGCCCA", 1))))
	if swapped.Status != IdentityNotIdentified || swapped.Reference != nil || swapped.Seqcol.Digest != changedDigest || swapped.Reason == "" {
		t.Errorf("swapped = %+v", swapped)
	}
}

func TestCompressedFastaHasTheSameIdentityAsPlain(t *testing.T) {
	dir := t.TempDir()
	got := identify(t, write(t, dir, "known.fa.gz", gzipped(t, []byte(identifyText))))
	if got.Status != IdentityIdentified || got.Seqcol.Digest != identifyDigest {
		t.Errorf("got = %+v", got)
	}
}

func TestIdentifyNeverFailsOnWhatItCannotName(t *testing.T) {
	dir := t.TempDir()
	cases := map[string][]byte{
		"reads.fastq": fastqText("SRR1", 5, "ACGT"),
		"empty.fa":    nil,
	}
	for base, data := range cases {
		got := identify(t, write(t, dir, base, data))
		if got.Status != IdentityNotIdentified || got.Reason == "" || got.Reference != nil {
			t.Errorf("%s: %+v", base, got)
		}
		if code := (IdentifyReport{Files: []*Identification{got}}).ExitCode(); code != ExitOK {
			t.Errorf("%s: exit %d", base, code)
		}
	}
}

func TestBrokenFastaIsInvalidAndExits3(t *testing.T) {
	dir := t.TempDir()
	cases := map[string][]byte{
		"broken.fa":    []byte(">a\nACGT\n;comment\n"),
		"truncated.fa": []byte(">a\nACGT"),
		"nameless.fa":  []byte(">\nACGT\n"),
	}
	for base, data := range cases {
		got := identify(t, write(t, dir, base, data))
		if got.Status != StatusInvalid || !strings.Contains(got.Reason, "not valid") || got.Seqcol != nil || got.Reference != nil {
			t.Errorf("%s: %+v", base, got)
		}
		if code := (IdentifyReport{Files: []*Identification{got}}).ExitCode(); code != ExitIncomplete {
			t.Errorf("%s: exit %d, want 3", base, code)
		}
	}
}

func TestUnreadableOutranksInvalidInTheExitCode(t *testing.T) {
	report := IdentifyReport{Files: []*Identification{
		{Status: StatusInvalid}, {Status: StatusUnreadable}, {Status: IdentityNotIdentified},
	}}
	if code := report.ExitCode(); code != ExitFatal {
		t.Errorf("exit %d, want 2", code)
	}
}

func TestMissingFileIsUnreadableAndFatal(t *testing.T) {
	got := identify(t, secret.NewName(t.TempDir()+"/absent.fa"))
	if got.Status != StatusUnreadable || got.Reason == "" {
		t.Errorf("got = %+v", got)
	}
	if code := (IdentifyReport{Files: []*Identification{got}}).ExitCode(); code != ExitFatal {
		t.Errorf("exit %d", code)
	}
	if strings.Contains(got.Reason, "absent.fa") {
		t.Errorf("the reason leaks the path: %s", got.Reason)
	}
}

func TestFileChangedWhileIdentifyingIsNeverIdentified(t *testing.T) {
	data := []byte(strings.Repeat(">a\n"+strings.Repeat("ACGT", 1000)+"\n", 3000))
	path := t.TempDir() + "/sample.fa"
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	src := &changingSource{File: f, at: len(data) / 2, change: func() {
		if err := os.WriteFile(path, append(data[:len(data)-1:len(data)-1], 'X'), 0o644); err != nil {
			t.Error(err)
		}
	}}
	got, err := identifySource(context.Background(), &Identification{Name: secret.NewName(path)}, src, tinyReferences)
	if err != nil {
		t.Fatal(err)
	}
	if !src.fired || got.Status != StatusUnreadable || !strings.Contains(got.Reason, "changed while it was being read") || got.Seqcol != nil {
		t.Errorf("fired=%v got=%+v", src.fired, got)
	}
}

func TestIdentifyJSONHasItsOwnSchema(t *testing.T) {
	dir := t.TempDir()
	report, err := IdentifyAll(context.Background(), []secret.Name{
		write(t, dir, "known.fa", []byte(identifyText)),
		write(t, dir, "reads.fastq", fastqText("SRR1", 3, "ACGT")),
	}, tinyReferences)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	out := bytes.NewBuffer(encoded)
	var decoded struct {
		Files []struct {
			Path      string `json:"path"`
			Status    string `json:"status"`
			Reference *struct {
				Name   string `json:"name"`
				Source string `json:"source"`
			} `json:"reference"`
			Seqcol *struct {
				Digest string `json:"digest"`
			} `json:"seqcol"`
		} `json:"files"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if report.ExitCode() != ExitOK || len(decoded.Files) != 2 {
		t.Fatalf("decoded = %+v", decoded)
	}
	if f := decoded.Files[0]; f.Status != "identified" || f.Reference == nil || f.Reference.Name != "tiny" || f.Seqcol.Digest != identifyDigest {
		t.Errorf("first = %+v", f)
	}
	if f := decoded.Files[1]; f.Status != "not_identified" || f.Seqcol != nil || f.Reference != nil {
		t.Errorf("second = %+v", f)
	}
}

func TestIdentifyAnswerCarriesTheDigestAndTheSequenceCount(t *testing.T) {
	dir := t.TempDir()
	report, err := IdentifyAll(context.Background(), []secret.Name{write(t, dir, "known.fa", []byte(identifyText))}, tinyReferences)
	if err != nil {
		t.Fatal(err)
	}
	got := report.Files[0]
	if got.Status != IdentityIdentified || got.Reference.Label != "tiny" || got.Seqcol.Digest != identifyDigest || got.Seqcol.Sequences != 3 {
		t.Errorf("answer %+v %+v", got, got.Seqcol)
	}
}
