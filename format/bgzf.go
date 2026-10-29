package format

import (
	"encoding/binary"
	"fmt"
)

const (
	gzipFixedHeader = 12
	gzipTrailer     = 8
)

type blockWalker struct {
	head  []byte
	skip  int64
	start int64
	next  int64
	bad   *problem
}

func (w *blockWalker) fail(reason string) {
	w.bad = &problem{reason: reason, offset: w.start}
}

func (w *blockWalker) Write(p []byte) {
	for len(p) > 0 && w.bad == nil {
		if w.skip > 0 {
			n := int(min(int64(len(p)), w.skip))
			w.skip -= int64(n)
			p = p[n:]
			continue
		}
		if len(w.head) == 0 {
			w.start = w.next
		}
		if len(w.head) < gzipFixedHeader {
			take := min(len(p), gzipFixedHeader-len(w.head))
			w.head = append(w.head, p[:take]...)
			p = p[take:]
			if len(w.head) < gzipFixedHeader {
				continue
			}
		}
		need := gzipFixedHeader + int(binary.LittleEndian.Uint16(w.head[10:]))
		take := min(len(p), need-len(w.head))
		w.head = append(w.head, p[:take]...)
		p = p[take:]
		if len(w.head) == need {
			w.openBlock()
		}
	}
}

func (w *blockWalker) openBlock() {
	h := w.head
	if h[0] != gzipMagic[0] || h[1] != gzipMagic[1] || h[2] != 8 || h[3]&0x04 == 0 {
		w.fail("no BGZF block header where the block sizes place one")
		return
	}
	size, ok := blockSize(h[gzipFixedHeader:])
	if !ok {
		w.fail("BGZF block has no BC subfield")
		return
	}
	if size < int64(len(h))+gzipTrailer {
		w.fail(fmt.Sprintf("BGZF block size %d is smaller than its own header and trailer", size))
		return
	}
	w.skip = size - int64(len(h))
	w.next = w.start + size
	w.head = w.head[:0]
}

func blockSize(extra []byte) (int64, bool) {
	for len(extra) >= 4 {
		slen := int(binary.LittleEndian.Uint16(extra[2:]))
		if 4+slen > len(extra) {
			return 0, false
		}
		if extra[0] == 'B' && extra[1] == 'C' && slen == 2 {
			return int64(binary.LittleEndian.Uint16(extra[4:])) + 1, true
		}
		extra = extra[4+slen:]
	}
	return 0, false
}

func (w *blockWalker) finish() *problem {
	if w.bad == nil && (w.skip > 0 || len(w.head) > 0) {
		w.fail("BGZF block sizes run past the end of the file")
	}
	return w.bad
}
