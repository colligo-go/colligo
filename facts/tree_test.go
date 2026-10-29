package facts

import (
	"bytes"
	"math/rand"
	"testing"

	"lukechampine.com/blake3"
	"lukechampine.com/blake3/bao"
)

func treeSizes() []int {
	return []int{
		0, 1, 63, 64, 1023, 1024, 1025, 16 * 1024, 16*1024 + 1,
		GroupSize - 1, GroupSize, GroupSize + 1,
		2 * GroupSize, 2*GroupSize + 7, 3 * GroupSize, 3*GroupSize - 1, 5*GroupSize + 12345,
	}
}

func buildTree(data []byte, writeSize int) *treeBuilder {
	tb := &treeBuilder{}
	for off := 0; off < len(data); off += writeSize {
		tb.Write(data[off:min(off+writeSize, len(data))])
	}
	return tb
}

func TestTreeRootMatchesBlake3(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, size := range treeSizes() {
		data := make([]byte, size)
		rng.Read(data)
		want := blake3.Sum256(data)
		for _, writeSize := range []int{1 << 20, 4099, 1 << 24} {
			root, groups := buildTree(data, writeSize).sum()
			if root != want {
				t.Fatalf("size %d write %d: root %x, want %x", size, writeSize, root, want)
			}
			if got := RootFromGroups(groups); got != want {
				t.Fatalf("size %d: root from groups %x, want %x", size, got, want)
			}
			if wantGroups := max(1, (size+GroupSize-1)/GroupSize); len(groups) != wantGroups {
				t.Fatalf("size %d: %d groups, want %d", size, len(groups), wantGroups)
			}
		}
	}
}

func TestOutboardMatchesBao(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for _, size := range treeSizes() {
		data := make([]byte, size)
		rng.Read(data)
		root, groups := buildTree(data, 1<<20).sum()
		outboard := Outboard(int64(size), groups)
		if len(outboard) != bao.EncodedSize(size, GroupLog, true) {
			t.Fatalf("size %d: outboard %d bytes, want %d", size, len(outboard), bao.EncodedSize(size, GroupLog, true))
		}
		wantOutboard, wantRoot := bao.EncodeBuf(data, GroupLog, true)
		if root != wantRoot || !bytes.Equal(outboard, wantOutboard) {
			t.Fatalf("size %d: outboard differs from bao", size)
		}
		if !bao.VerifyBuf(data, outboard, GroupLog, root) {
			t.Fatalf("size %d: bao rejects our outboard", size)
		}
	}
}

func TestGroupsTileTheFile(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 20; i++ {
		size := rng.Intn(4 * GroupSize)
		tb := buildTree(make([]byte, size), 1<<20)
		_, groups := tb.sum()
		last := size - (len(groups)-1)*GroupSize
		if size > 0 && (last <= 0 || last > GroupSize) {
			t.Fatalf("size %d: last group %d bytes", size, last)
		}
	}
}
