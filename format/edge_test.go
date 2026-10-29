package format

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func validatorFor(t *testing.T, label string) FormatValidator {
	t.Helper()
	v, ok := Lookup(label)
	if !ok {
		t.Fatalf("no validator for %s", label)
	}
	return v
}

func recordCounts(t *testing.T, facts map[string]json.RawMessage) (records, bases int64) {
	t.Helper()
	var r struct {
		Records int64 `json:"records"`
		Bases   int64 `json:"bases"`
	}
	if err := json.Unmarshal(facts["records"], &r); err != nil {
		t.Fatal(err)
	}
	return r.Records, r.Bases
}

var chunkSizes = []int{1, 7, 4096, 1 << 20}

func TestFastqEdgeCases(t *testing.T) {
	v := validatorFor(t, ContentFastq)
	cases := []struct {
		name    string
		data    string
		valid   bool
		records int64
	}{
		{"separator repeats the ID", "@r1\nACGT\n+r1\nIIII\n@r2\nAC\n+r2\nII\n", true, 2},
		{"quality starts with at sign", "@r1\nACGT\n+\n@III\n@r2\nACGT\n+\n@@@@\n", true, 2},
		{"quality that looks like a header", "@r1\nAC\n+\n@r\n@r2\nAC\n+\nII\n", true, 2},
		{"separator that is only a plus and spaces", "@r1\nAC\n+ \nII\n", true, 1},
		{"sequence line starting with plus", "@r1\n+C\n+\nII\n", true, 1},
		{"empty sequence and empty quality", "@r1\n\n+\n\n", true, 1},
		{"header only", "@r1\n", false, 0},
		{"UTF-8 BOM before the first header", "\xef\xbb\xbf@r1\nAC\n+\nII\n", false, 0},
	}
	for _, tc := range cases {
		for _, chunk := range chunkSizes {
			t.Run(fmt.Sprintf("%s/chunk %d", tc.name, chunk), func(t *testing.T) {
				facts := extract(t, v, tc.data, chunk)
				s := structureOf(t, facts)
				if s.Valid != tc.valid {
					t.Fatalf("valid = %v (%s), want %v", s.Valid, s.Reason, tc.valid)
				}
				if tc.valid {
					if n, _ := recordCounts(t, facts); n != tc.records {
						t.Fatalf("records = %d, want %d", n, tc.records)
					}
				}
			})
		}
	}
}

func TestUTF8BOMAtTheStartIsSniffedButRejected(t *testing.T) {
	cases := map[string]string{
		ContentFastq: "@r1\nAC\n+\nII\n",
		ContentFasta: ">s\nAC\n",
		ContentGTF:   gtfLine(9, 0) + "\n",
	}
	for label, body := range cases {
		data := "\xef\xbb\xbf" + body
		if got := DetectContent([]byte(data)); got != label {
			t.Fatalf("%s: sniffed as %s", label, got)
		}
		for _, chunk := range chunkSizes {
			s := structureOf(t, extract(t, validatorFor(t, label), data, chunk))
			if s.Valid || s.Offset != 0 || !strings.Contains(s.Reason, "byte order mark") {
				t.Fatalf("%s chunk %d: %+v", label, chunk, s)
			}
		}
		if !structureOf(t, extract(t, validatorFor(t, label), body, 1)).Valid {
			t.Fatalf("%s: the same content without the BOM must stay valid", label)
		}
	}
}

func TestUTF8BOMOnlyMattersAtTheVeryStart(t *testing.T) {
	facts := extract(t, validatorFor(t, ContentFasta), ">a\xef\xbb\xbf\nAC\n", 1)
	if s := structureOf(t, facts); !s.Valid {
		t.Fatalf("%+v", s)
	}
	for _, partial := range []string{"\xef", "\xef\xbb", "\xef\xbb\xbf"} {
		s := structureOf(t, extract(t, validatorFor(t, ContentFasta), partial, 1))
		if s.Valid {
			t.Fatalf("%q accepted", partial)
		}
	}
	if s := structureOf(t, extract(t, validatorFor(t, ContentFasta), "\xef\xbb\xbf", 1)); !strings.Contains(s.Reason, "byte order mark") {
		t.Fatalf("%+v", s)
	}
}

func TestFastaEdgeCases(t *testing.T) {
	v := validatorFor(t, ContentFasta)
	cases := []struct {
		name    string
		data    string
		valid   bool
		records int64
		bases   int64
	}{
		{"header without sequence", ">a\n", true, 1, 0},
		{"empty sequence between records", ">a\n>b\nACGT\n>c\n", true, 3, 4},
		{"lowercase and mixed case are counted", ">a\nacgtnACGTN\nacgt\n", true, 1, 14},
		{"one byte header marker", ">", false, 0, 0},
		{"one byte sequence", "A", false, 0, 0},
		{"one byte newline", "\n", false, 0, 0},
		{"no trailing newline", ">a\nACGT", false, 0, 0},
		{"blank lines inside a record", ">a\nAC\n\nGT\n", true, 1, 4},
		{"UTF-8 BOM before the header", "\xef\xbb\xbf>a\nAC\n", false, 0, 0},
	}
	for _, tc := range cases {
		for _, chunk := range chunkSizes {
			t.Run(fmt.Sprintf("%s/chunk %d", tc.name, chunk), func(t *testing.T) {
				facts := extract(t, v, tc.data, chunk)
				s := structureOf(t, facts)
				if s.Valid != tc.valid {
					t.Fatalf("valid = %v (%s), want %v", s.Valid, s.Reason, tc.valid)
				}
				if tc.valid {
					if n, b := recordCounts(t, facts); n != tc.records || b != tc.bases {
						t.Fatalf("records, bases = %d, %d; want %d, %d", n, b, tc.records, tc.bases)
					}
				}
			})
		}
	}
}

func gtfLine(fields int, total int) string {
	cols := []string{"chr1", "HAVANA", "gene", "1", "100", ".", "+", ".", "gene_id \"G\";"}
	line := strings.Join(cols[:min(fields, len(cols))], "\t")
	for len(strings.Split(line, "\t")) < fields {
		line += "\tx"
	}
	if pad := total - len(line); pad > 0 {
		line += strings.Repeat("a", pad)
	}
	return line
}

func TestGTFEdgeCases(t *testing.T) {
	v := validatorFor(t, ContentGTF)
	cases := []struct {
		name    string
		data    string
		valid   bool
		records int64
	}{
		{"only comments", "#!genome-build GRCh38\n#another\n", false, 0},
		{"nine fields", gtfLine(9, 0) + "\n", true, 1},
		{"eight fields", gtfLine(8, 0) + "\n", false, 0},
		{"ten fields", gtfLine(10, 0) + "\n", false, 0},
		{"comments around features", "#a\n" + gtfLine(9, 0) + "\n#b\n" + gtfLine(9, 0) + "\n", true, 2},
		{"line of exactly 64 KiB", gtfLine(9, maxKeptLine) + "\n", true, 1},
		{"line of 64 KiB plus one byte", gtfLine(9, maxKeptLine+1) + "\n", false, 0},
		{"line of exactly 64 KiB with CRLF", gtfLine(9, maxKeptLine) + "\r\n", true, 1},
		{"line of 64 KiB plus one byte with CRLF", gtfLine(9, maxKeptLine+1) + "\r\n", false, 0},
		{"feature after a long comment", "#" + strings.Repeat("c", 3*maxKeptLine) + "\n" + gtfLine(9, 0) + "\n", true, 1},
	}
	for _, tc := range cases {
		for _, chunk := range chunkSizes {
			t.Run(fmt.Sprintf("%s/chunk %d", tc.name, chunk), func(t *testing.T) {
				facts := extract(t, v, tc.data, chunk)
				s := structureOf(t, facts)
				if s.Valid != tc.valid {
					t.Fatalf("valid = %v (%s), want %v", s.Valid, s.Reason, tc.valid)
				}
				if tc.valid {
					if n, _ := recordCounts(t, facts); n != tc.records {
						t.Fatalf("records = %d, want %d", n, tc.records)
					}
				}
			})
		}
	}
}

func bgzfVerdict(t *testing.T, data []byte, chunk int) structureValue {
	t.Helper()
	e := bgzfValidator{}.NewExtractor()
	for rest := data; len(rest) > 0; {
		n := min(chunk, len(rest))
		e.Write(rest[:n])
		rest = rest[n:]
	}
	facts, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	var s structureValue
	if err := json.Unmarshal(facts[0].Value, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBGZFBlockSizesMustChainToTheEndOfTheFile(t *testing.T) {
	a, b, c := bgzfBlock(t, []byte("first")), bgzfBlock(t, []byte("second")), bgzfBlock(t, []byte("third"))
	eof := BGZFEOFBlock()
	join := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	withSize := func(block []byte, delta int) []byte {
		out := bytes.Clone(block)
		binary.LittleEndian.PutUint16(out[16:], uint16(int(binary.LittleEndian.Uint16(out[16:]))+delta))
		return out
	}
	plainMember := func() []byte {
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		w.Write([]byte("no BC field"))
		w.Close()
		return buf.Bytes()
	}()
	cases := []struct {
		name   string
		data   []byte
		valid  bool
		offset int64
	}{
		{"well formed", join(a, b, c, eof), true, 0},
		{"size too small in the first block", join(withSize(a, -3), b, c, eof), false, int64(len(a) - 3)},
		{"size too large in the middle block", join(a, withSize(b, 3), c, eof), false, int64(len(a) + len(b) + 3)},
		{"size too large in the last data block", join(a, b, withSize(c, 3), eof), false, int64(len(a) + len(b) + len(c) + 3)},
		{"size past the end of the file", join(a, withSize(b, 0x7000)), false, int64(len(a))},
		{"plain gzip member between blocks", join(a, plainMember, c, eof), false, int64(len(a))},
		{"cut in the middle of a block", join(a, b[:len(b)/2]), false, int64(len(a))},
		{"cut in the middle of a header", join(a, b[:7]), false, int64(len(a))},
		{"size smaller than header and trailer", join(withSize(a, -len(a)+20), b, c, eof), false, 0},
		{"EOF block in the middle", join(a, eof, b, eof), true, 0},
		{"two EOF blocks at the end", join(a, eof, eof), true, 0},
	}
	for _, tc := range cases {
		for _, chunk := range []int{1, 5, 29, 1 << 20} {
			t.Run(fmt.Sprintf("%s/chunk %d", tc.name, chunk), func(t *testing.T) {
				s := bgzfVerdict(t, tc.data, chunk)
				if s.Valid != tc.valid {
					t.Fatalf("valid = %v (%s), want %v", s.Valid, s.Reason, tc.valid)
				}
				if !tc.valid && s.Offset != tc.offset {
					t.Fatalf("offset = %d, want %d (%s)", s.Offset, tc.offset, s.Reason)
				}
			})
		}
	}
}
