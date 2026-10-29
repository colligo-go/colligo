package verifier

import (
	"bytes"
	"encoding/json"

	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/model"
)

func quickStructureFact(version, label string, valid bool, reason string, offset int64) model.Fact {
	raw, _ := json.Marshal(structureFact{Format: label, Valid: valid, Reason: reason, Offset: offset})
	return model.Fact{Kind: "structure", ExtractorVersion: version, Value: raw}
}

func bgzfTailFact(tail []byte) model.Fact {
	v, _ := format.Lookup(format.ContainerBgzf)
	if bytes.Equal(tail, format.BGZFEOFBlock()) {
		return quickStructureFact(v.Version(), format.ContainerBgzf, true, "", 0)
	}
	return quickStructureFact(v.Version(), format.ContainerBgzf, false, "missing BGZF EOF block", 0)
}

func textTailFact(content string, last byte) model.Fact {
	label := content
	if label == "" {
		label = format.ContentUnknown
	}
	version := label + "-1"
	if v, ok := format.Lookup(label); ok {
		version = v.Version()
	}
	if last == '\n' {
		return quickStructureFact(version, label, true, "", 0)
	}
	return quickStructureFact(version, label, false, "last line is incomplete", 0)
}
