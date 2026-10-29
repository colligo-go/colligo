package format

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
)

func extract(t *testing.T, v FormatValidator, data string, chunk int) map[string]json.RawMessage {
	t.Helper()
	e := v.NewExtractor()
	for len(data) > 0 {
		n := min(chunk, len(data))
		if _, err := e.Write([]byte(data[:n])); err != nil {
			t.Fatal(err)
		}
		data = data[n:]
	}
	facts, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]json.RawMessage{}
	for _, f := range facts {
		out[f.Kind] = f.Value
	}
	return out
}

func structureOf(t *testing.T, facts map[string]json.RawMessage) structureValue {
	t.Helper()
	var s structureValue
	if err := json.Unmarshal(facts["structure"], &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func fastq(records int, seq string) string {
	var b strings.Builder
	for i := 0; i < records; i++ {
		fmt.Fprintf(&b, "@read%d/1 extra\n%s\n+\n%s\n", i, seq, strings.Repeat("I", len(seq)))
	}
	return b.String()
}

func TestFastqValid(t *testing.T) {
	data := fastq(10, "ACGTACGT")
	for _, chunk := range []int{1, 3, 7, 4096} {
		facts := extract(t, fastqValidator{}, data, chunk)
		if s := structureOf(t, facts); !s.Valid {
			t.Fatalf("chunk %d: %+v", chunk, s)
		}
		var rec recordsValue
		if err := json.Unmarshal(facts["records"], &rec); err != nil {
			t.Fatal(err)
		}
		if rec.Records != 10 || rec.Bases != 80 || rec.Lengths[8] != 10 {
			t.Fatalf("chunk %d: records = %+v", chunk, rec)
		}
		var ends readEndsValue
		if err := json.Unmarshal(facts["read_ends"], &ends); err != nil {
			t.Fatal(err)
		}
		if ends.First != "read0" || ends.Last != "read9" {
			t.Fatalf("chunk %d: ends = %+v", chunk, ends)
		}
	}
}

func TestFastqDetectsStructuralProblems(t *testing.T) {
	good := fastq(3, "ACGT")
	cases := map[string]string{
		"truncated mid-line":       good[:len(good)-3],
		"truncated at line break":  good[:strings.LastIndex(strings.TrimSuffix(good, "\n"), "\n")+1],
		"missing trailing newline": strings.TrimSuffix(good, "\n"),
		"quality too short":        "@r\nACGT\n+\nIII\n",
		"bad header":               "r\nACGT\n+\nIIII\n",
		"bad separator":            "@r\nACGT\nx\nIIII\n",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			s := structureOf(t, extract(t, fastqValidator{}, data, 5))
			if s.Valid {
				t.Fatal("not detected")
			}
			if s.Reason == "" {
				t.Fatal("no reason")
			}
		})
	}
}

func TestFastqPairSuffixStripped(t *testing.T) {
	for _, header := range []string{"@SRR1.1/1", "@SRR1.1 1:N:0:ATCACG", "@SRR1.1 1 length=4"} {
		facts := extract(t, fastqValidator{}, header+"\nACGT\n+\nIIII\n", 3)
		var ends readEndsValue
		if err := json.Unmarshal(facts["read_ends"], &ends); err != nil {
			t.Fatal(err)
		}
		if ends.First != "SRR1.1" {
			t.Errorf("%q -> %q", header, ends.First)
		}
	}
}

func TestFastqZeroAllocationsPerRecord(t *testing.T) {
	e := newFastqExtractor()
	block := []byte(fastq(1000, strings.Repeat("ACGT", 25)))
	if _, err := e.Write(block); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(50, func() { e.Write(block) })
	if allocs != 0 {
		t.Fatalf("allocs per Write of 1000 records = %v, want 0", allocs)
	}
}

func TestFastqRecordSplitAcrossWritesIsZeroAlloc(t *testing.T) {
	e := newFastqExtractor()
	data := []byte(fastq(200, strings.Repeat("ACGT", 25)))
	writeAll := func() {
		for off := 0; off < len(data); off += 97 {
			e.Write(data[off:min(off+97, len(data))])
		}
	}
	writeAll()
	if allocs := testing.AllocsPerRun(20, writeAll); allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestFasta(t *testing.T) {
	good := ">chr1 desc\nACGT\nAC\n>chr2\nGG\n"
	facts := extract(t, fastaValidator{}, good, 4)
	if !structureOf(t, facts).Valid {
		t.Fatal("valid fasta rejected")
	}
	var names seqNamesValue
	if err := json.Unmarshal(facts["seq_names"], &names); err != nil {
		t.Fatal(err)
	}
	if names.Convention != conventionUCSC || len(names.Names) != 2 {
		t.Fatalf("%+v", names)
	}
	for _, bad := range []string{">chr1\nACGT", "ACGT\n", ">\nAC\n", ""} {
		if structureOf(t, extract(t, fastaValidator{}, bad, 3)).Valid {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestGTF(t *testing.T) {
	line := "1\tsrc\tgene\t1\t10\t.\t+\t.\tgene_id \"g\";\n"
	good := "#!genome-build X\n" + line + line
	facts := extract(t, gtfValidator{}, good, 6)
	if !structureOf(t, facts).Valid {
		t.Fatal("valid gtf rejected")
	}
	var names seqNamesValue
	if err := json.Unmarshal(facts["seq_names"], &names); err != nil {
		t.Fatal(err)
	}
	if names.Convention != conventionEnsembl {
		t.Fatalf("%+v", names)
	}
	for _, bad := range []string{line[:len(line)-4], line + "1\tonly\tthree\n", line + "x\ty\n"} {
		if structureOf(t, extract(t, gtfValidator{}, bad, 6)).Valid {
			t.Errorf("%q accepted", bad)
		}
	}
}

func bgzfBlock(t testing.TB, payload []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Extra = []byte{'B', 'C', 2, 0, 0, 0}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw := b.Bytes()
	binary.LittleEndian.PutUint16(raw[16:], uint16(len(raw)-1))
	return raw
}

func bgzfBlocks(t testing.TB, n int) []byte {
	t.Helper()
	var out []byte
	for i := 0; i < n; i++ {
		out = append(out, bgzfBlock(t, bytes.Repeat([]byte{byte(i)}, 40))...)
	}
	return out
}

func TestContainerBgzfEOFBlock(t *testing.T) {
	v := bgzfValidator{}
	for _, withEOF := range []bool{true, false} {
		e := v.NewExtractor()
		e.Write(bgzfBlocks(t, 3))
		if withEOF {
			e.Write(BGZFEOFBlock())
		}
		facts, err := e.Finish()
		if err != nil {
			t.Fatal(err)
		}
		var s structureValue
		if err := json.Unmarshal(facts[0].Value, &s); err != nil {
			t.Fatal(err)
		}
		if s.Valid != withEOF {
			t.Errorf("withEOF=%v: %+v", withEOF, s)
		}
	}
}

func TestContainerPayloadError(t *testing.T) {
	e := gzipValidator{}.NewExtractor()
	e.(PayloadReporter).ReportPayload(10, 5, fmt.Errorf("unexpected EOF"))
	facts, _ := e.Finish()
	var s structureValue
	json.Unmarshal(facts[0].Value, &s)
	if s.Valid || s.Reason != "unexpected EOF" || s.Offset != 10 {
		t.Fatalf("%+v", s)
	}
}

func TestDetection(t *testing.T) {
	if DetectContainer([]byte{0x1f, 0x8b, 8, 0}) != ContainerGzip {
		t.Error("gzip")
	}
	bgzfHead := []byte{0x1f, 0x8b, 8, 4, 0, 0, 0, 0, 0, 0xff, 6, 0, 'B', 'C', 2, 0, 0x1b, 0}
	if DetectContainer(bgzfHead) != ContainerBgzf {
		t.Error("bgzf")
	}
	if DetectContainer([]byte("<html>")) != ContainerNone {
		t.Error("none")
	}
	for content, want := range map[string]string{
		"@r\nA\n+\nI\n":               ContentFastq,
		">s\nAC\n":                    ContentFasta,
		"#!x\n":                       ContentGTF,
		"1\ta\tb\tc\td\te\tf\tg\th\n": ContentGTF,
		"<!DOCTYPE html>":             ContentHTML,
		"  <html>":                    ContentHTML,
		"random":                      ContentUnknown,
		"":                            ContentUnknown,
	} {
		if got := DetectContent([]byte(content)); got != want {
			t.Errorf("%q: got %s want %s", content, got, want)
		}
	}
}

func TestFromNameAndPairKey(t *testing.T) {
	c, f := FromName(secret.NewName("dir/x.fastq.gz"))
	if c != ContainerGzip || f != ContentFastq {
		t.Errorf("%s %s", c, f)
	}
	type want struct {
		mate int
		ok   bool
	}
	cases := map[string]want{
		"a_R1.fastq.gz":           {1, true},
		"a_R2.fastq.gz":           {2, true},
		"SRR2133898_1.fastq.gz":   {1, true},
		"SRR2133898_2.fastq.gz":   {2, true},
		"s.1.fq":                  {1, true},
		"S1_L001_R1_001.fastq.gz": {1, true},
		"plain.fastq.gz":          {0, false},
	}
	for name, w := range cases {
		_, mate, ok := PairKey(secret.NewName(name))
		if mate != w.mate || ok != w.ok {
			t.Errorf("%s: %d %v", name, mate, ok)
		}
	}
	s1, _, _ := PairKey(secret.NewName("SRR2133898_1.fastq.gz"))
	s2, _, _ := PairKey(secret.NewName("SRR2133898_2.fastq.gz"))
	if s1 != s2 {
		t.Errorf("stems differ: %q %q", s1, s2)
	}
	r1, _, _ := PairKey(secret.NewName("S1_L001_R1_001.fastq.gz"))
	r2, _, _ := PairKey(secret.NewName("S1_L001_R2_001.fastq.gz"))
	if r1 != r2 {
		t.Errorf("stems differ: %q %q", r1, r2)
	}
}

func TestRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range Registry() {
		if seen[v.Version()] {
			t.Errorf("duplicate version %s", v.Version())
		}
		seen[v.Version()] = true
		if !Known(v.Format()) {
			t.Errorf("%s not known", v.Format())
		}
	}
	if Known("nope") {
		t.Error("nope known")
	}
	var _ model.Fact
}

func TestBGZFTailIsFoundWhateverTheWriteSplit(t *testing.T) {
	body := bgzfBlocks(t, 4)
	full := append(append([]byte{}, body...), BGZFEOFBlock()...)
	valid := func(parts ...[]byte) bool {
		e := bgzfValidator{}.NewExtractor()
		for _, p := range parts {
			e.Write(p)
		}
		facts, err := e.Finish()
		if err != nil {
			t.Fatal(err)
		}
		var s structureValue
		if err := json.Unmarshal(facts[0].Value, &s); err != nil {
			t.Fatal(err)
		}
		return s.Valid
	}
	for i := 0; i <= len(full); i++ {
		for j := i; j <= len(full); j += 7 {
			if !valid(full[:i], full[i:j], full[j:]) {
				t.Fatalf("EOF block lost when split at %d and %d", i, j)
			}
		}
	}
	for i := 0; i <= len(body); i += 5 {
		if valid(body[:i], body[i:]) {
			t.Fatalf("a file without the EOF block was accepted (split %d)", i)
		}
	}
}

func TestFastqCRLFCountsBasesWithoutTheCarriageReturn(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 4; i++ {
		fmt.Fprintf(&b, "@r%d/1\r\nACGTACGTAC\r\n+\r\nFFFFFFFFFF\r\n", i)
	}
	for _, chunk := range []int{1, 5, 4096} {
		facts := extract(t, fastqValidator{}, b.String(), chunk)
		if !structureOf(t, facts).Valid {
			t.Fatalf("chunk %d: CRLF file rejected", chunk)
		}
		var rec recordsValue
		if err := json.Unmarshal(facts["records"], &rec); err != nil {
			t.Fatal(err)
		}
		if rec.Records != 4 || rec.Bases != 40 || rec.Lengths[10] != 4 {
			t.Fatalf("chunk %d: %+v", chunk, rec)
		}
		var ends readEndsValue
		json.Unmarshal(facts["read_ends"], &ends)
		if ends.Last != "r3" {
			t.Fatalf("chunk %d: ends %+v", chunk, ends)
		}
	}
}

func TestFastaCRLF(t *testing.T) {
	facts := extract(t, fastaValidator{}, ">chr1\r\nACGT\r\nAC\r\n", 3)
	var rec struct {
		Bases int64 `json:"bases"`
	}
	json.Unmarshal(facts["records"], &rec)
	if !structureOf(t, facts).Valid || rec.Bases != 6 {
		t.Fatalf("%+v", rec)
	}
}

func TestOneEnormousLineNeverFillsMemory(t *testing.T) {
	for name, v := range map[string]FormatValidator{"fasta": fastaValidator{}, "fastq": fastqValidator{}, "gtf": gtfValidator{}} {
		e := v.NewExtractor()
		first := map[string]string{"fasta": ">chr1\n", "fastq": "@r\n", "gtf": ""}[name]
		e.Write([]byte(first))
		block := bytes.Repeat([]byte("A"), 1<<20)
		for i := 0; i < 64; i++ {
			e.Write(block)
		}
		var kept int
		switch x := e.(type) {
		case *fastaExtractor:
			kept = cap(x.lines.carry)
		case *fastqExtractor:
			kept = cap(x.lines.carry)
		case *gtfExtractor:
			kept = cap(x.lines.carry)
		}
		if kept > 2*maxKeptLine {
			t.Errorf("%s: kept %d bytes of a line that has no end", name, kept)
		}
	}
}

func TestFastaOneLineSequenceIsCounted(t *testing.T) {
	e := newFastaExtractor()
	e.Write([]byte(">chr1\n"))
	block := bytes.Repeat([]byte("ACGT"), 1<<18)
	for i := 0; i < 8; i++ {
		e.Write(block)
	}
	e.Write([]byte("\n"))
	facts, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Bases int64 `json:"bases"`
	}
	for _, f := range facts {
		if f.Kind == "records" {
			json.Unmarshal(f.Value, &rec)
		}
	}
	if rec.Bases != 8*int64(len(block)) {
		t.Fatalf("bases = %d", rec.Bases)
	}
}

func TestGTFLineLongerThanTheKeptPrefixIsRejected(t *testing.T) {
	long := "1\tsrc\tgene\t1\t10\t.\t+\t.\t" + strings.Repeat("x", maxKeptLine+10) + "\n"
	if structureOf(t, extract(t, gtfValidator{}, long, 1<<20)).Valid {
		t.Fatal("a feature line whose fields cannot be counted was accepted")
	}
}

func TestLengthHistogramIsBounded(t *testing.T) {
	var b strings.Builder
	const reads = maxLengthBuckets + 500
	for i := 0; i < reads; i++ {
		n := 10 + i
		fmt.Fprintf(&b, "@r%d\n%s\n+\n%s\n", i, strings.Repeat("A", n), strings.Repeat("F", n))
	}
	facts := extract(t, fastqValidator{}, b.String(), 1<<16)
	var rec recordsValue
	if err := json.Unmarshal(facts["records"], &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Records != reads || len(rec.Lengths) != maxLengthBuckets || rec.Overflow != 500 {
		t.Fatalf("records %d buckets %d overflow %d", rec.Records, len(rec.Lengths), rec.Overflow)
	}
}

func TestFastqTrailingBlankLinesAreValidButCounted(t *testing.T) {
	data := fastq(3, "ACGT") + "\n\n"
	for _, chunk := range []int{1, 7, 4096} {
		s := structureOf(t, extract(t, fastqValidator{}, data, chunk))
		if !s.Valid || s.TrailingBlankLines != 2 {
			t.Fatalf("chunk %d: %+v", chunk, s)
		}
	}
	if s := structureOf(t, extract(t, fastqValidator{}, fastq(3, "ACGT"), 7)); s.TrailingBlankLines != 0 {
		t.Fatalf("no blank lines but counted %d", s.TrailingBlankLines)
	}
}

func TestFastqBlankLineInsideIsInvalid(t *testing.T) {
	good := fastq(3, "ACGT")
	for name, data := range map[string]string{
		"between records":  fastq(1, "ACGT") + "\n" + fastq(2, "ACGT"),
		"before the first": "\n" + good,
		"inside a record":  "@r\n\nACGT\n+\nIIII\n",
	} {
		s := structureOf(t, extract(t, fastqValidator{}, data, 5))
		if s.Valid {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFastaSemicolonLinesAreRejected(t *testing.T) {
	for _, data := range []string{">chr1\n;old comment\nACGT\n", ";header comment\n>chr1\nACGT\n"} {
		s := structureOf(t, extract(t, fastaValidator{}, data, 4))
		if s.Valid || !strings.Contains(s.Reason, "';'") {
			t.Errorf("%q: %+v", data, s)
		}
	}
}
