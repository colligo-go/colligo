package facts

import (
	"bytes"
	"context"
	"testing"

	"github.com/colligo-go/colligo/format"
)

func FuzzComputeGzip(f *testing.F) {
	f.Add(gz(f, []byte("@r/1\nACGT\n+\nIIII\n")))
	f.Add(gz(f, []byte(">chr1\nACGT\n")))
	f.Add([]byte{0x1f, 0x8b, 8, 0})
	f.Add([]byte("<html>"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := Compute(context.Background(), bytes.NewReader(data), Options{Hashes: true})
		if err != nil {
			t.Fatal(err)
		}
		if res.Size != int64(len(data)) {
			t.Fatalf("size %d of %d", res.Size, len(data))
		}
		if res.Content != "" && res.Content != format.ContentUnknown && res.Content != format.ContentHTML && !format.Known(res.Content) {
			t.Fatalf("content %q is outside the registry", res.Content)
		}
		if RootFromGroups(res.Groups) != res.Root {
			t.Fatal("root differs from the one derived from the groups")
		}
	})
}
