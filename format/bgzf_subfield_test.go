package format

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"testing"
)

func bgzfBlockWithExtra(t *testing.T, payload, extra []byte, bcValueAt int) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Extra = extra
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw := b.Bytes()
	if bcValueAt >= 0 {
		binary.LittleEndian.PutUint16(raw[gzipFixedHeader+bcValueAt:], uint16(len(raw)-1))
	}
	return raw
}

func TestBGZFBlockSizeIsFoundAfterAnotherExtraSubfield(t *testing.T) {
	extra := []byte{'X', 'Y', 3, 0, 9, 9, 9, 'B', 'C', 2, 0, 0, 0}
	a := bgzfBlockWithExtra(t, []byte("first"), extra, 11)
	b := bgzfBlockWithExtra(t, []byte("second"), extra, 11)
	data := bytes.Join([][]byte{a, b, BGZFEOFBlock()}, nil)
	if got := DetectContainer(a); got != ContainerBgzf {
		t.Fatalf("container %q, want bgzf", got)
	}
	for _, chunk := range []int{1, 5, 29, 1 << 20} {
		if s := bgzfVerdict(t, data, chunk); !s.Valid {
			t.Fatalf("chunk %d: rejected (%s)", chunk, s.Reason)
		}
	}
}

func TestBGZFBlockWithoutABCSubfieldIsRejectedAtThatBlock(t *testing.T) {
	good := bgzfBlock(t, []byte("first"))
	cases := []struct {
		name  string
		extra []byte
	}{
		{"only another subfield", []byte{'X', 'Y', 2, 0, 1, 2}},
		{"BC with the wrong length", []byte{'B', 'C', 3, 0, 1, 2, 3}},
		{"subfield longer than the extra field", []byte{'X', 'Y', 9, 0, 1, 2}},
	}
	for _, tc := range cases {
		bad := bgzfBlockWithExtra(t, []byte("second"), tc.extra, -1)
		data := bytes.Join([][]byte{good, bad, BGZFEOFBlock()}, nil)
		if got := DetectContainer(bad); got == ContainerBgzf {
			t.Errorf("%s: detected as bgzf", tc.name)
		}
		for _, chunk := range []int{1, 5, 1 << 20} {
			t.Run(fmt.Sprintf("%s/chunk %d", tc.name, chunk), func(t *testing.T) {
				s := bgzfVerdict(t, data, chunk)
				if s.Valid {
					t.Fatal("accepted a block whose size cannot be read")
				}
				if s.Offset != int64(len(good)) || s.Reason != "BGZF block has no BC subfield" {
					t.Fatalf("offset %d reason %q", s.Offset, s.Reason)
				}
			})
		}
	}
}
