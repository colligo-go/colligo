package facts

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/klauspost/compress/gzip"

	"github.com/colligo-go/colligo/format"
)

func bgzf(t testing.TB, data []byte, withEOF bool) []byte {
	t.Helper()
	var out bytes.Buffer
	for len(data) > 0 {
		n := min(len(data), 32<<10)
		var block bytes.Buffer
		w := gzip.NewWriter(&block)
		w.Extra = []byte{'B', 'C', 2, 0, 0, 0}
		if _, err := w.Write(data[:n]); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		raw := block.Bytes()
		binary.LittleEndian.PutUint16(raw[16:], uint16(len(raw)-1))
		out.Write(raw)
		data = data[n:]
	}
	if withEOF {
		out.Write(format.BGZFEOFBlock())
	}
	return out.Bytes()
}

func TestBGZFEndToEnd(t *testing.T) {
	raw := makeFastq(2000)
	for _, withEOF := range []bool{true, false} {
		data := bgzf(t, raw, withEOF)
		res, err := Compute(context.Background(), bytes.NewReader(data), Options{Hashes: true})
		if err != nil {
			t.Fatal(err)
		}
		if res.Container != "bgzf" || res.Content != "fastq" {
			t.Fatalf("container %s content %s", res.Container, res.Content)
		}
		if got := allValid(t, res); got != withEOF {
			t.Fatalf("withEOF=%v: valid=%v %+v", withEOF, got, structures(t, res))
		}
	}
}
