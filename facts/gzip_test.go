package facts

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"testing"

	"github.com/colligo-go/colligo/format"
)

func gzipMember(t testing.TB, data []byte, header func(*gzip.Writer)) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if header != nil {
		header(w)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func computeBytes(t testing.TB, data []byte) Result {
	t.Helper()
	res, err := Compute(context.Background(), bytes.NewReader(data), Options{Hashes: true})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func structureOf(t *testing.T, r Result, label string) structure {
	t.Helper()
	for _, s := range structures(t, r) {
		if s.Format == label {
			return s
		}
	}
	t.Fatalf("no structure fact for %s", label)
	return structure{}
}

func mutate(data []byte, edit func([]byte)) []byte {
	out := append([]byte(nil), data...)
	edit(out)
	return out
}

func TestTamperedGzipIsInvalid(t *testing.T) {
	raw := makeFastq(2000)
	member := gz(t, raw)
	cases := map[string][]byte{
		"crc altered":   mutate(member, func(b []byte) { b[len(b)-8] ^= 0xff }),
		"isize altered": mutate(member, func(b []byte) { b[len(b)-1] ^= 0x01 }),
		"deflate byte flipped in the middle": mutate(member, func(b []byte) {
			b[10+(len(b)-18)/2] ^= 0x10
		}),
		"garbage between members":       append(append(append([]byte(nil), member...), "garbage!"...), member...),
		"garbage after the last member": append(append([]byte(nil), member...), "garbage!"...),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			res := computeBytes(t, data)
			if allValid(t, res) {
				t.Fatalf("tampered gzip accepted: %+v", structures(t, res))
			}
			if container := structureOf(t, res, format.ContainerGzip); container.Valid {
				t.Fatalf("container verdict = %+v", container)
			}
		})
	}
}

func TestLegitimateGzipVariantsAreValid(t *testing.T) {
	raw := makeFastq(500)
	half := len(raw) / 2
	for half > 0 && raw[half-1] != '\n' {
		half++
	}
	cases := map[string][]byte{
		"empty member between two members": bytes.Join([][]byte{
			gzipMember(t, raw[:half], nil), gzipMember(t, nil, nil), gzipMember(t, raw[half:], nil)}, nil),
		"empty member first": append(gzipMember(t, nil, nil), gzipMember(t, raw, nil)...),
		"empty member last":  append(gzipMember(t, raw, nil), gzipMember(t, nil, nil)...),
		"FNAME and FCOMMENT": gzipMember(t, raw, func(w *gzip.Writer) {
			w.Name, w.Comment = "reads.fastq", "made by a test"
		}),
		"FEXTRA that is not BGZF": gzipMember(t, raw, func(w *gzip.Writer) {
			w.Extra = []byte{'X', 'Y', 2, 0, 1, 2}
		}),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			res := computeBytes(t, data)
			if res.Container != format.ContainerGzip || res.Content != format.ContentFastq {
				t.Fatalf("container %s content %s", res.Container, res.Content)
			}
			if !allValid(t, res) {
				t.Fatalf("legitimate gzip rejected: %+v", structures(t, res))
			}
		})
	}
}

func TestGzipOfNothingIsAValidContainerWithoutContent(t *testing.T) {
	res := computeBytes(t, gzipMember(t, nil, nil))
	ss := structures(t, res)
	if len(ss) != 1 || !ss[0].Valid || ss[0].Format != format.ContainerGzip {
		t.Fatalf("%+v", ss)
	}
	if res.Content != format.ContentUnknown {
		t.Fatalf("content = %q", res.Content)
	}
}

func lieAboutFirstBlockSize(data []byte, delta int) []byte {
	return mutate(data, func(b []byte) {
		size := binary.LittleEndian.Uint16(b[16:])
		binary.LittleEndian.PutUint16(b[16:], uint16(int(size)+delta))
	})
}

func TestBGZFStructure(t *testing.T) {
	raw := makeFastq(2000)
	eof := format.BGZFEOFBlock()
	half := len(raw) / 2
	for half > 0 && raw[half-1] != '\n' {
		half++
	}
	cases := []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"well formed", bgzf(t, raw, true), true},
		{"BSIZE larger than the block", lieAboutFirstBlockSize(bgzf(t, raw, true), 5), false},
		{"BSIZE smaller than the block", lieAboutFirstBlockSize(bgzf(t, raw, true), -5), false},
		{"EOF block in the middle and none at the end",
			bytes.Join([][]byte{bgzf(t, raw[:half], false), eof, bgzf(t, raw[half:], false)}, nil), false},
		{"EOF block in the middle and at the end",
			bytes.Join([][]byte{bgzf(t, raw[:half], false), eof, bgzf(t, raw[half:], true)}, nil), true},
		{"two EOF blocks at the end", append(bgzf(t, raw, true), eof...), true},
		{"no EOF block", bgzf(t, raw, false), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := computeBytes(t, tc.data)
			if res.Container != format.ContainerBgzf {
				t.Fatalf("container = %s", res.Container)
			}
			if got := allValid(t, res); got != tc.valid {
				t.Fatalf("valid = %v, want %v: %+v", got, tc.valid, structures(t, res))
			}
		})
	}
}

func TestComputeRejectsAByteOrderMarkPlainAndInsideGzip(t *testing.T) {
	data := append([]byte("\xef\xbb\xbf"), makeFastq(10)...)
	for name, input := range map[string][]byte{"plain": data, "gzip": gz(t, data)} {
		t.Run(name, func(t *testing.T) {
			res := computeBytes(t, input)
			if res.Content != format.ContentFastq {
				t.Fatalf("content = %s", res.Content)
			}
			if structureOf(t, res, format.ContentFastq).Valid {
				t.Fatal("byte order mark accepted")
			}
		})
	}
}
