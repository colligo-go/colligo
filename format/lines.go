package format

import "bytes"

const maxKeptLine = 64 << 10

type lineReader struct {
	carry  []byte
	length int
	last   byte
	pos    int64
	handle func(prefix []byte, length int, offset int64)
}

func (l *lineReader) keep(seg []byte) {
	if room := maxKeptLine - len(l.carry); room > 0 {
		l.carry = append(l.carry, seg[:min(room, len(seg))]...)
	}
}

func (l *lineReader) write(p []byte) {
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		seg := p
		if i >= 0 {
			seg = p[:i]
		}
		if len(seg) > 0 {
			l.last = seg[len(seg)-1]
		}
		if i < 0 {
			l.keep(seg)
			l.length += len(seg)
			return
		}
		prefix := seg[:min(len(seg), maxKeptLine)]
		if l.length > 0 {
			l.keep(seg)
			prefix = l.carry
		}
		raw := l.length + len(seg)
		length := raw
		if length > 0 && l.last == '\r' {
			length--
			prefix = prefix[:min(len(prefix), length)]
		}
		l.handle(prefix, length, l.pos)
		l.pos += int64(raw) + 1
		l.carry, l.length, l.last = l.carry[:0], 0, 0
		p = p[i+1:]
	}
}

func (l *lineReader) partial() int { return l.length }
