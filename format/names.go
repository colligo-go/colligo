package format

import (
	"regexp"

	"github.com/colligo-go/colligo/model"
)

const (
	sampledNames      = 64
	trackedNames      = 100000
	conventionUCSC    = "ucsc"
	conventionEnsembl = "ensembl"
	conventionMixed   = "mixed"
	conventionUnknown = "unknown"
)

var ensemblName = regexp.MustCompile(`^([0-9]+|X|Y|MT|M)$`)

type nameSampler struct {
	seen    map[string]struct{}
	sample  []string
	capped  bool
	ucsc    int
	ensembl int
}

func (s *nameSampler) add(name []byte) {
	if s.seen == nil {
		s.seen = make(map[string]struct{})
	}
	if _, ok := s.seen[string(name)]; ok {
		return
	}
	if len(s.seen) >= trackedNames {
		s.capped = true
		return
	}
	n := string(name)
	s.seen[n] = struct{}{}
	if len(s.sample) < sampledNames {
		s.sample = append(s.sample, n)
	}
	switch {
	case len(n) > 3 && n[:3] == "chr":
		s.ucsc++
	case ensemblName.MatchString(n):
		s.ensembl++
	}
}

func (s *nameSampler) convention() string {
	switch {
	case s.ucsc > 0 && s.ensembl > 0:
		return conventionMixed
	case s.ucsc > 0:
		return conventionUCSC
	case s.ensembl > 0:
		return conventionEnsembl
	}
	return conventionUnknown
}

type seqNamesValue struct {
	Names      []string `json:"names"`
	Distinct   int      `json:"distinct"`
	Capped     bool     `json:"capped"`
	Convention string   `json:"convention"`
}

func (s *nameSampler) fact(version string) (model.Fact, error) {
	names := s.sample
	if names == nil {
		names = []string{}
	}
	return jsonFact("seq_names", version, seqNamesValue{
		Names: names, Distinct: len(s.seen), Capped: s.capped, Convention: s.convention(),
	})
}
