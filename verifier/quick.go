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
	if bytes.Equal(tail, format.BGZFEOFBlock()) {
		return quickStructureFact("bgzf-1", format.ContainerBgzf, true, "", 0)
	}
	return quickStructureFact("bgzf-1", format.ContainerBgzf, false, "missing BGZF EOF block", 0)
}

func textTailFact(content string, last byte) model.Fact {
	label := content
	if label == "" {
		label = format.ContentUnknown
	}
	if last == '\n' {
		return quickStructureFact(label+"-1", label, true, "", 0)
	}
	return quickStructureFact(label+"-1", label, false, "last line is incomplete", 0)
}
