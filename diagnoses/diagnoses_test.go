package diagnoses_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
)

func TestEveryEntryHasACompleteLongExplanation(t *testing.T) {
	for _, id := range diagnoses.IDs() {
		e, ok := diagnoses.Explain(id)
		if !ok {
			t.Errorf("%s has no long explanation", id)
			continue
		}
		for name, text := range map[string]string{"Means": e.Means, "Why": e.Why, "Do": e.Do, "Confirm": e.Confirm} {
			if len(strings.Fields(text)) < 5 {
				t.Errorf("%s: %s is empty or too short: %q", id, name, text)
			}
		}
	}
}

func TestNoExplanationBelongsToAnIdThatDoesNotExist(t *testing.T) {
	for _, id := range []string{"no_such_entry", ""} {
		if _, ok := diagnoses.Explain(id); ok {
			t.Errorf("an explanation exists for %q", id)
		}
	}
	for _, id := range diagnoses.IDs() {
		if _, ok := diagnoses.Lookup(id); !ok {
			t.Errorf("%s is listed but cannot be looked up", id)
		}
	}
}

func TestSimilarFindsTheIdsAPersonMeansToType(t *testing.T) {
	cases := map[string][]string{
		"count":          {"count_mismatch", "pair_count"},
		"count_mismtach": {"count_mismatch"},
		"pair":           {"pair_count", "pair_ids", "pair_missing"},
		"COUNT_MISMATCH": {"count_mismatch"},
		"checksum":       {"checksum_mismatch"},
	}
	for typed, want := range cases {
		got := diagnoses.Similar(typed, 8)
		for _, id := range want {
			if !slices.Contains(got, id) {
				t.Errorf("Similar(%q) = %v, lacks %s", typed, got, id)
			}
		}
	}
	if got := diagnoses.Similar("zzzzzzzzzz", 5); len(got) != 0 {
		t.Errorf("nothing resembles zzzzzzzzzz, got %v", got)
	}
	if got := diagnoses.Similar("pair", 2); len(got) != 2 {
		t.Errorf("the limit is ignored: %v", got)
	}
	if got := diagnoses.Similar("", 5); len(got) != 0 {
		t.Errorf("an empty id resembles nothing, got %v", got)
	}
}
