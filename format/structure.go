package format

import (
	"encoding/json"

	"github.com/colligo-go/colligo/model"
)

type structureValue struct {
	Format string `json:"format"`
	Valid  bool   `json:"valid"`
	Reason string `json:"reason"`
	Offset int64  `json:"offset"`
}

type problem struct {
	reason string
	offset int64
}

func structureFact(version, label string, p *problem) (model.Fact, error) {
	v := structureValue{Format: label, Valid: p == nil}
	if p != nil {
		v.Reason, v.Offset = p.reason, p.offset
	}
	return jsonFact("structure", version, v)
}

func jsonFact(kind, version string, v any) (model.Fact, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return model.Fact{}, err
	}
	return model.Fact{Kind: kind, ExtractorVersion: version, Value: raw}, nil
}
