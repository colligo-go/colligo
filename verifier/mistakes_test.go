package verifier

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/model"
)

func bgzfOf(t *testing.T, payload []byte) []byte {
	t.Helper()
	var block bytes.Buffer
	w := gzip.NewWriter(&block)
	w.Extra = []byte{'B', 'C', 2, 0, 0, 0}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw := block.Bytes()
	binary.LittleEndian.PutUint16(raw[16:], uint16(len(raw)-1))
	return append(raw, format.BGZFEOFBlock()...)
}

func bamBytes(t *testing.T) []byte {
	return bgzfOf(t, append([]byte("BAM\x01"), make([]byte, 64)...))
}

func TestACommonMistakeIsNamedAndStaysBlocking(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		data     []byte
		wantID   string
		observed string
	}{
		{"reads.sra", append([]byte("NCBI.sra"), make([]byte, 64)...), diagnoses.InputSRA, "SRA"},
		{"aligned.bam", bamBytes(t), diagnoses.InputAlignment, "BAM"},
		{"aligned.cram", append([]byte("CRAM\x03\x00"), make([]byte, 64)...), diagnoses.InputAlignment, "CRAM"},
		{"bundle.zip", append([]byte("PK\x03\x04"), make([]byte, 64)...), diagnoses.InputArchive, "ZIP"},
		{"empty.zip", []byte("PK\x05\x06" + string(make([]byte, 18))), diagnoses.InputArchive, "ZIP"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := run(t, Options{}, write(t, dir, c.name, c.data))
			file := rep.Files[0]
			if file.Status != StatusInvalid {
				t.Errorf("status %q: a file Colligo cannot check must never read verified", file.Status)
			}
			if got := failed(file); !slices.Contains(got, c.wantID) || slices.Contains(got, diagnoses.FormatUnknown) {
				t.Errorf("failed assertions %v, want %s and not format_unknown", got, c.wantID)
			}
			if code := rep.ExitCode(false); code != ExitIncomplete {
				t.Errorf("exit %d, want %d", code, ExitIncomplete)
			}
			for _, a := range file.Assertions {
				if a.ID == c.wantID && a.Severity != model.Blocking {
					t.Errorf("%s has severity %s, want blocking as format_unknown had", a.ID, a.Severity)
				}
			}
		})
	}
}

func TestAnUnrecognisedBinaryFileIsStillFormatUnknown(t *testing.T) {
	rep := run(t, Options{}, write(t, t.TempDir(), "mystery.dat", []byte{0, 1, 2, 3, 4, 5}))
	if got := failed(rep.Files[0]); !slices.Contains(got, diagnoses.FormatUnknown) {
		t.Errorf("failed %v, want format_unknown", got)
	}
}

func TestACommonMistakeIsNamedInAQuickCheckToo(t *testing.T) {
	rep := run(t, Options{Quick: true}, write(t, t.TempDir(), "reads.sra", append([]byte("NCBI.sra"), make([]byte, 64)...)))
	if got := failed(rep.Files[0]); !slices.Contains(got, diagnoses.InputSRA) {
		t.Errorf("failed %v, want input_sra", got)
	}
}
