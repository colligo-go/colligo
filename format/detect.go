package format

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/colligo-go/colligo/secret"
)

var (
	gzipMagic = []byte{0x1f, 0x8b}
	bgzfEOF   = []byte{
		0x1f, 0x8b, 0x08, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x06, 0x00, 0x42, 0x43, 0x02, 0x00,
		0x1b, 0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
)

func BGZFEOFBlock() []byte { return append([]byte(nil), bgzfEOF...) }

func DetectContainer(head []byte) string {
	if !bytes.HasPrefix(head, gzipMagic) {
		return ContainerNone
	}
	if len(head) >= 18 && head[3]&0x04 != 0 {
		xlen := int(head[10]) | int(head[11])<<8
		extra := head[12:]
		if len(extra) > xlen {
			extra = extra[:xlen]
		}
		for len(extra) >= 4 {
			slen := int(extra[2]) | int(extra[3])<<8
			if extra[0] == 'B' && extra[1] == 'C' && slen == 2 {
				return ContainerBgzf
			}
			if 4+slen > len(extra) {
				break
			}
			extra = extra[4+slen:]
		}
	}
	return ContainerGzip
}

func DetectContent(head []byte) string {
	trimmed := bytes.TrimLeft(head, " \t\r\n")
	if len(trimmed) == 0 {
		return ContentUnknown
	}
	switch trimmed[0] {
	case '<':
		return ContentHTML
	case '@':
		return ContentFastq
	case '>':
		return ContentFasta
	case '#':
		return ContentGTF
	}
	line := head
	if i := bytes.IndexByte(head, '\n'); i >= 0 {
		line = head[:i]
	}
	if bytes.Count(line, []byte{'\t'}) == 8 {
		return ContentGTF
	}
	return ContentUnknown
}

// FromName infers container and content from a file name. The result is
// only a hint: content always wins, and divergence is advisory.
func FromName(name secret.Name) (container, content string) {
	base := strings.ToLower(filepath.Base(name.String()))
	container = ContainerNone
	switch {
	case strings.HasSuffix(base, ".bgz"):
		container = ContainerBgzf
		base = strings.TrimSuffix(base, ".bgz")
	case strings.HasSuffix(base, ".gz"):
		container = ContainerGzip
		base = strings.TrimSuffix(base, ".gz")
	}
	switch filepath.Ext(base) {
	case ".fastq", ".fq":
		content = ContentFastq
	case ".fasta", ".fa", ".fna":
		content = ContentFasta
	case ".gtf":
		content = ContentGTF
	}
	return container, content
}
