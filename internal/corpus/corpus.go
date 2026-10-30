// Package corpus generates the versioned synthetic corpus in
// testdata/synthetic. Each fixture carries what the generator knows it wrote,
// so the expected counts and failures never come from a parser. Compression
// uses klauspost/compress, pinned in go.sum, because the bytes of the standard
// library compressor change between Go releases. Generation must stay
// deterministic: a change in the generator has to show up as a diff.
package corpus

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"strings"

	"github.com/klauspost/compress/gzip"
)

type Expect struct {
	Records *int64   `json:"records,omitempty"`
	Bases   *int64   `json:"bases,omitempty"`
	Failed  []string `json:"failed"`
	Skipped bool     `json:"skipped,omitempty"`
}

type Fixture struct {
	Label       string
	Description string
	Data        []byte
	Expect      Expect
}

type reads struct {
	text    bytes.Buffer
	records int64
	bases   int64
}

func (r *reads) add(id string, seq string, eol string) {
	fmt.Fprintf(&r.text, "@%s%s%s%s+%s%s%s", id, eol, seq, eol, eol, strings.Repeat("F", len(seq)), eol)
	r.records++
	r.bases += int64(len(seq))
}

func randomReads(seed int64, prefix, mate string, n, minLen, maxLen int) *reads {
	rng := rand.New(rand.NewSource(seed))
	r := &reads{}
	for i := 0; i < n; i++ {
		seq := make([]byte, minLen+rng.Intn(maxLen-minLen+1))
		for j := range seq {
			seq[j] = "ACGTN"[rng.Intn(5)]
		}
		r.add(fmt.Sprintf("%s.%d%s", prefix, i+1, mate), string(seq), "\n")
	}
	return r
}

func gz(data []byte) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write(data)
	w.Close()
	return b.Bytes()
}

var bgzfEOF = []byte{
	0x1f, 0x8b, 0x08, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x06, 0x00, 0x42, 0x43, 0x02, 0x00,
	0x1b, 0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
}

func bgzf(data []byte, withEOF bool) []byte {
	var out bytes.Buffer
	for len(data) > 0 {
		n := min(len(data), 32<<10)
		var block bytes.Buffer
		w := gzip.NewWriter(&block)
		w.Extra = []byte{'B', 'C', 2, 0, 0, 0}
		w.Write(data[:n])
		w.Close()
		raw := block.Bytes()
		binary.LittleEndian.PutUint16(raw[16:], uint16(len(raw)-1))
		out.Write(raw)
		data = data[n:]
	}
	if withEOF {
		out.Write(bgzfEOF)
	}
	return out.Bytes()
}

func withBlockSizeOff(data []byte, delta int) []byte {
	out := bytes.Clone(data)
	size := binary.LittleEndian.Uint16(out[16:])
	binary.LittleEndian.PutUint16(out[16:], uint16(int(size)+delta))
	return out
}

func count(n int64) *int64 { return &n }

func valid(label, description string, data []byte, r *reads, failed ...string) Fixture {
	if failed == nil {
		failed = []string{}
	}
	return Fixture{Label: label, Description: description, Data: data,
		Expect: Expect{Records: count(r.records), Bases: count(r.bases), Failed: failed}}
}

func broken(label, description string, data []byte, failed ...string) Fixture {
	return Fixture{Label: label, Description: description, Data: data, Expect: Expect{Failed: failed}}
}

func Generate() []Fixture {
	plain := randomReads(1, "SRR100", "/1", 200, 50, 110)

	pair1 := randomReads(2, "PAIR", "/1", 300, 76, 76)
	pair2 := randomReads(3, "PAIR", "/2", 300, 76, 76)

	orphan := randomReads(9, "ORPHAN", "/1", 80, 60, 60)

	desync1 := randomReads(4, "DSYNC", "/1", 120, 60, 60)
	desync2 := randomReads(5, "DSYNC", "/2", 119, 60, 60)

	crlf := &reads{}
	for i := 0; i < 40; i++ {
		crlf.add(fmt.Sprintf("CRLF.%d", i+1), "ACGTACGTAC", "\r\n")
	}

	blank := randomReads(6, "BLANK", "", 30, 40, 40)
	blankData := append(append([]byte{}, blank.text.Bytes()...), '\n', '\n')

	truncatedGz := gz(randomReads(7, "TRUNC", "", 2000, 100, 100).text.Bytes())

	bgz := randomReads(8, "BGZF", "", 500, 90, 90)

	lengths := &reads{}
	for i := 0; i < 4096+200; i++ {
		lengths.add(fmt.Sprintf("LEN.%d", i+1), strings.Repeat("A", 5+i), "\n")
	}

	genome := ">chr1 synthetic\nACGTACGTAC\nACGTAC\n>chr2\nGGGGCCCC\n"
	annotation := "#!genome-build synthetic\n" +
		"chr1\tsynthetic\tgene\t1\t16\t.\t+\t.\tgene_id \"g1\";\n" +
		"chr1\tsynthetic\texon\t1\t10\t.\t+\t.\tgene_id \"g1\";\n" +
		"chr2\tsynthetic\tgene\t1\t8\t.\t-\t.\tgene_id \"g2\";\n"
	annotationRecords := int64(3)

	return []Fixture{
		valid("plain.fastq", "FASTQ with reads of varied length", plain.text.Bytes(), plain),
		valid("pair_R1.fastq.gz", "first mate of a consistent pair", gz(pair1.text.Bytes()), pair1),
		valid("pair_R2.fastq.gz", "second mate of a consistent pair", gz(pair2.text.Bytes()), pair2),
		valid("desync_R1.fastq", "first mate of a pair whose second mate lost one read", desync1.text.Bytes(), desync1, "pair_count"),
		valid("desync_R2.fastq", "second mate with one read fewer than the first", desync2.text.Bytes(), desync2, "pair_count"),
		valid("orphan_R1.fastq", "first mate whose second mate is not in the corpus", orphan.text.Bytes(), orphan, "pair_missing"),
		valid("crlf.fastq", "FASTQ with CRLF line endings; \\r is not a base", crlf.text.Bytes(), crlf),
		valid("trailing-blank.fastq", "complete FASTQ followed by two blank lines", blankData, blank, "trailing_blank_lines"),
		valid("bgzf.fastq.bgz", "FASTQ in BGZF with the EOF block", bgzf(bgz.text.Bytes(), true), bgz),
		valid("many-lengths.fastq.gz", "more distinct read lengths than the histogram keeps", gz(lengths.text.Bytes()), lengths),
		{Label: "genome.fa", Description: "two-sequence FASTA", Data: []byte(genome),
			Expect: Expect{Records: count(2), Bases: count(24), Failed: []string{}}},
		{Label: "annotation.gtf", Description: "GTF with a pragma and three features", Data: []byte(annotation),
			Expect: Expect{Records: &annotationRecords, Failed: []string{}}},
		broken("truncated.fastq.gz", "gzip cut at two thirds of its length", truncatedGz[:len(truncatedGz)*2/3], "structure_invalid"),
		broken("bgzf-no-eof.fastq.bgz", "BGZF without the EOF block", bgzf(bgz.text.Bytes(), false), "structure_invalid"),
		broken("bgzf-bad-bsize.fastq.bgz", "BGZF whose first block claims five bytes more than it has", withBlockSizeOff(bgzf(bgz.text.Bytes(), true), 5), "structure_invalid"),
		broken("bom.gtf", "valid GTF behind a UTF-8 byte order mark", append([]byte("\xef\xbb\xbf"), annotation...), "structure_invalid"),
		broken("html.gtf.gz", "an error page saved under a data name", []byte("<!DOCTYPE html><html><body>503 Service Unavailable</body></html>\n"), "html_body"),
		broken("not-gzip.fastq.gz", "plain FASTQ text named as gzip", plain.text.Bytes(), "magic_mismatch"),
		broken("semicolon.fa", "FASTA with an old ';' comment line", []byte(">chr1\n;comment\nACGT\n"), "structure_invalid"),
		broken("empty.fastq", "an empty file named as FASTQ", nil, "format_unknown"),
		{Label: "notes.txt", Description: "not data: discovery must skip it", Data: []byte("lab notes\n"),
			Expect: Expect{Failed: []string{}, Skipped: true}},
	}
}
