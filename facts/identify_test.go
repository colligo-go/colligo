package facts

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/identity"
)

const identifyFasta = ">chrA first record\nACGTACGT\nacgtNN\n>chrB\nGGGGCCCC\n\n>chrC desc\nTTTT\n"

const identifyFastaDigest = "-ILNzNQsKYl_9KDXtkCWYUl5t5NESUdA"

func bigFasta() []byte {
	var b bytes.Buffer
	for i := 0; i < 40; i++ {
		b.WriteString(">contig")
		b.WriteByte(byte('a' + i%26))
		b.WriteByte(byte('a' + i/26))
		b.WriteByte('\n')
		for line := 0; line < 2000; line++ {
			b.WriteString(strings.Repeat("ACGTTGCA", 8))
			b.WriteByte('\n')
		}
	}
	return b.Bytes()
}

func seqcolOf(t *testing.T, r Result) identity.SeqcolValue {
	t.Helper()
	return factValue[identity.SeqcolValue](t, r, identity.FactKind)
}

func TestIdentifyAddsTheSeqcolFactToThePlainRead(t *testing.T) {
	res, err := Compute(context.Background(), strings.NewReader(identifyFasta), Options{Identify: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := seqcolOf(t, res); got.Digest != identifyFastaDigest || got.Sequences != 3 {
		t.Errorf("seqcol = %+v", got)
	}
	fact, _ := res.Fact(identity.FactKind)
	if fact.ExtractorVersion != identity.ExtractorVersion {
		t.Errorf("version = %s", fact.ExtractorVersion)
	}
}

func TestSeqcolFactIsAbsentUnlessAsked(t *testing.T) {
	res, err := Compute(context.Background(), strings.NewReader(identifyFasta), Options{Hashes: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Fact(identity.FactKind); ok {
		t.Error("check must not pay for the seqcol digest")
	}
}

func TestSeqcolDigestIsTheSameThroughGzipAndBgzf(t *testing.T) {
	data := bigFasta()
	plain, err := Compute(context.Background(), bytes.NewReader(data), Options{Identify: true})
	if err != nil {
		t.Fatal(err)
	}
	want := seqcolOf(t, plain)
	if want.Digest == "" || want.Sequences != 40 {
		t.Fatalf("plain seqcol = %+v", want)
	}
	for name, packed := range map[string][]byte{"gzip": gz(t, data), "bgzf": bgzf(t, data, true)} {
		res, err := Compute(context.Background(), bytes.NewReader(packed), Options{Identify: true, Hashes: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := seqcolOf(t, res); got != want {
			t.Errorf("%s: seqcol = %+v, want %+v", name, got, want)
		}
	}
}

func TestIdentifyReadsTheFileExactlyOnce(t *testing.T) {
	data := bigFasta()
	for name, content := range map[string][]byte{"plain": data, "gzip": gz(t, data)} {
		counter := &countingReader{r: bytes.NewReader(content)}
		res, err := Compute(context.Background(), counter, Options{Identify: true, Hashes: true})
		if err != nil {
			t.Fatal(err)
		}
		if counter.bytes != int64(len(content)) || res.Size != int64(len(content)) {
			t.Errorf("%s: read %d bytes of %d (size %d)", name, counter.bytes, len(content), res.Size)
		}
		if seqcolOf(t, res).Digest == "" {
			t.Errorf("%s: no digest", name)
		}
	}
}

func TestSeqcolFactIsOmittedWhenTheFastaIsInvalid(t *testing.T) {
	res, err := Compute(context.Background(), strings.NewReader(">a\nACGT\n;comment\n"), Options{Identify: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Fact(identity.FactKind); ok {
		t.Error("an invalid FASTA must not get a collection digest")
	}
}

func TestSeqcolFactIsOmittedForOtherFormats(t *testing.T) {
	res, err := Compute(context.Background(), bytes.NewReader(makeFastq(10)), Options{Identify: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Fact(identity.FactKind); ok {
		t.Error("FASTQ has no seqcol")
	}
}
