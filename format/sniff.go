package format

import (
	"bytes"
	"io"

	"github.com/klauspost/compress/gzip"
)

const sniffContentBytes = 4096

func Sniff(head []byte) (container, content string) {
	container = DetectContainer(head)
	if container == ContainerNone {
		return container, DetectContent(head)
	}
	gz, err := gzip.NewReader(bytes.NewReader(head))
	if err != nil {
		return container, ContentUnknown
	}
	buf := make([]byte, sniffContentBytes)
	n, _ := io.ReadFull(gz, buf)
	return container, DetectContent(buf[:n])
}
