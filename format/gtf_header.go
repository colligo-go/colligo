package format

import (
	"bytes"
	"strings"

	"github.com/colligo-go/colligo/identity"
)

const (
	maxHeaderValue = 1024
)

var biotypeKey = []byte("gene_biotype")

type gtfObserver struct {
	header      identity.GTFHeader
	inHeader    bool
	headerBytes int64
}

func newGTFObserver() *gtfObserver { return &gtfObserver{inHeader: true} }

func (o *gtfObserver) comment(line []byte, length int) {
	if !o.inHeader {
		return
	}
	o.headerBytes += int64(length) + 1
	if o.header.HeaderLines >= identity.MaxHeaderLines || o.headerBytes > identity.MaxHeaderBytes {
		o.header.Truncated = true
		o.inHeader = false
		return
	}
	o.header.HeaderLines++
	if length == 0 || length > len(line) || !bytes.HasPrefix(line, []byte("##")) {
		return
	}
	key, value, ok := strings.Cut(string(line[2:]), ":")
	if !ok {
		return
	}
	value = strings.TrimSpace(value)
	if len(value) > maxHeaderValue {
		value = value[:maxHeaderValue]
	}
	switch strings.TrimSpace(key) {
	case "description":
		setOnce(&o.header.Description, value)
	case "provider":
		setOnce(&o.header.Provider, value)
	case "format":
		setOnce(&o.header.Format, value)
	case "date":
		setOnce(&o.header.Date, value)
	}
}

func setOnce(field *string, value string) {
	if *field == "" {
		*field = value
	}
}

func (o *gtfObserver) feature(line []byte) {
	o.inHeader = false
	if !isGeneFeature(line) {
		return
	}
	attributes := line[bytes.LastIndexByte(line, '\t')+1:]
	for from := 0; ; {
		at := bytes.Index(attributes[from:], biotypeKey)
		if at < 0 {
			return
		}
		at += from
		end := at + len(biotypeKey)
		startsKey := at == 0 || attributes[at-1] == ' ' || attributes[at-1] == ';'
		endsKey := end < len(attributes) && (attributes[end] == ' ' || attributes[end] == '=')
		if startsKey && endsKey {
			o.header.GeneBiotypeLines++
			return
		}
		from = end
	}
}

func isGeneFeature(line []byte) bool {
	first := bytes.IndexByte(line, '\t')
	second := bytes.IndexByte(line[first+1:], '\t')
	if first < 0 || second < 0 {
		return false
	}
	rest := line[first+second+2:]
	return len(rest) > 4 && string(rest[:4]) == "gene" && rest[4] == '\t'
}
