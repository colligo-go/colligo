package facts

import (
	"encoding/binary"
	"math/bits"

	"lukechampine.com/blake3/guts"
)

const (
	GroupSize   = 8 << 20
	GroupLog    = 13
	groupChunks = GroupSize / guts.ChunkSize
)

type treeBuilder struct {
	buf        [guts.MaxSIMD * guts.ChunkSize]byte
	buflen     int
	stack      [groupChunks/guts.MaxSIMD + 1][8]uint32
	subtrees   uint64
	counter    uint64
	groupBytes int
	cvs        [][8]uint32
	total      int64
}

func (t *treeBuilder) Write(p []byte) (int, error) {
	n := len(p)
	t.total += int64(n)
	for len(p) > 0 {
		if t.groupBytes == GroupSize {
			t.closeGroup()
		}
		if t.buflen == len(t.buf) {
			t.pushSubtree()
		}
		take := min(len(p), len(t.buf)-t.buflen, GroupSize-t.groupBytes)
		copy(t.buf[t.buflen:], p[:take])
		t.buflen += take
		t.groupBytes += take
		p = p[take:]
	}
	return n, nil
}

func (t *treeBuilder) pushSubtree() {
	cv := guts.ChainingValue(guts.CompressBuffer(&t.buf, t.buflen, &guts.IV, t.counter+t.subtrees*guts.MaxSIMD, 0))
	i := 0
	for t.subtrees&(1<<i) != 0 {
		cv = guts.ChainingValue(guts.ParentNode(t.stack[i], cv, &guts.IV, 0))
		i++
	}
	t.stack[i] = cv
	t.subtrees++
	t.buflen = 0
}

func (t *treeBuilder) groupNode() guts.Node {
	n := guts.CompressBuffer(&t.buf, t.buflen, &guts.IV, t.counter+t.subtrees*guts.MaxSIMD, 0)
	for i := bits.TrailingZeros64(t.subtrees); i < bits.Len64(t.subtrees); i++ {
		if t.subtrees&(1<<i) != 0 {
			n = guts.ParentNode(t.stack[i], guts.ChainingValue(n), &guts.IV, 0)
		}
	}
	return n
}

func (t *treeBuilder) closeGroup() {
	t.cvs = append(t.cvs, guts.ChainingValue(t.groupNode()))
	t.counter += groupChunks
	t.subtrees = 0
	t.buflen = 0
	t.groupBytes = 0
}

func (t *treeBuilder) sum() (root [32]byte, groups [][32]byte) {
	last := t.groupNode()
	if len(t.cvs) == 0 {
		last.Flags |= guts.FlagRoot
		root = cvBytes(guts.ChainingValue(last))
		return root, [][32]byte{root}
	}
	cvs := append(append([][8]uint32(nil), t.cvs...), guts.ChainingValue(last))
	groups = make([][32]byte, len(cvs))
	for i, cv := range cvs {
		groups[i] = cvBytes(cv)
	}
	return cvBytes(mergeCVs(cvs, true)), groups
}

func mergeCVs(cvs [][8]uint32, root bool) [8]uint32 {
	if len(cvs) == 1 {
		return cvs[0]
	}
	mid := 1 << (bits.Len(uint(len(cvs)-1)) - 1)
	left := mergeCVs(cvs[:mid], false)
	right := mergeCVs(cvs[mid:], false)
	var flags uint32
	if root {
		flags = guts.FlagRoot
	}
	return guts.ChainingValue(guts.ParentNode(left, right, &guts.IV, flags))
}

func cvBytes(cv [8]uint32) (b [32]byte) {
	for i, w := range cv {
		binary.LittleEndian.PutUint32(b[4*i:], w)
	}
	return b
}

func bytesCV(b [32]byte) (cv [8]uint32) {
	for i := range cv {
		cv[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	return cv
}

func RootFromGroups(groups [][32]byte) [32]byte {
	if len(groups) == 1 {
		return groups[0]
	}
	cvs := make([][8]uint32, len(groups))
	for i, g := range groups {
		cvs[i] = bytesCV(g)
	}
	return cvBytes(mergeCVs(cvs, true))
}

func Outboard(size int64, groups [][32]byte) []byte {
	out := make([]byte, 8, 8+(2*len(groups))*32)
	binary.LittleEndian.PutUint64(out, uint64(size))
	cvs := make([][8]uint32, len(groups))
	for i, g := range groups {
		cvs[i] = bytesCV(g)
	}
	var emit func(cvs [][8]uint32)
	emit = func(cvs [][8]uint32) {
		if len(cvs) == 1 {
			return
		}
		mid := 1 << (bits.Len(uint(len(cvs)-1)) - 1)
		l := cvBytes(mergeCVs(cvs[:mid], false))
		r := cvBytes(mergeCVs(cvs[mid:], false))
		out = append(out, l[:]...)
		out = append(out, r[:]...)
		emit(cvs[:mid])
		emit(cvs[mid:])
	}
	emit(cvs)
	return out
}
