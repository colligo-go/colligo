package diagnoses

import (
	"sort"
	"strings"

	"github.com/colligo-go/colligo/internal/suggest"
)

// Similar returns catalogue ids close to id, nearest first: ids that contain
// it or share one of its words, then ids a few edits away. It is for the
// message of an id that does not exist.
func Similar(id string, limit int) []string {
	id = strings.ToLower(strings.TrimSpace(id))
	type scored struct {
		id    string
		score int
	}
	var found []scored
	for _, candidate := range IDs() {
		if score, ok := closeness(id, candidate); ok {
			found = append(found, scored{candidate, score})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].score < found[j].score })
	var out []string
	for _, f := range found {
		if len(out) == limit {
			break
		}
		out = append(out, f.id)
	}
	return out
}

func closeness(id, candidate string) (int, bool) {
	if id == "" {
		return 0, false
	}
	if strings.Contains(candidate, id) || strings.Contains(id, candidate) {
		return 0, true
	}
	for _, word := range strings.FieldsFunc(id, func(r rune) bool { return r == '_' || r == '-' }) {
		if len(word) >= 3 && strings.Contains(candidate, word) {
			return 1, true
		}
	}
	if d := suggest.Distance(id, candidate); d <= max(3, len(candidate)/4) {
		return 2 + d, true
	}
	return 0, false
}
