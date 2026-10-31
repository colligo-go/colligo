package suggest_test

import (
	"testing"

	"github.com/colligo-go/colligo/internal/suggest"
)

func TestDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0}, {"source", "source", 0}, {"sorce", "source", 1}, {"kitten", "sitting", 3}, {"", "abc", 3},
	}
	for _, c := range cases {
		if got := suggest.Distance(c.a, c.b); got != c.want {
			t.Errorf("Distance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestClosestNamesTheFlagThePersonMeant(t *testing.T) {
	flags := []string{"color", "json", "recursive", "samplesheet", "source"}
	cases := map[string]string{"sorce": "source", "recursve": "recursive", "colour": "color", "jsno": "json", "samplesheat": "samplesheet"}
	for typed, want := range cases {
		got, ok := suggest.Closest(typed, flags)
		if !ok || got != want {
			t.Errorf("Closest(%q) = %q, %v; want %q", typed, got, ok, want)
		}
	}
}

func TestClosestStaysQuietWhenNothingIsNear(t *testing.T) {
	if got, ok := suggest.Closest("zzzzzzzz", []string{"source", "json"}); ok {
		t.Errorf("suggested %q for gibberish", got)
	}
	if _, ok := suggest.Closest("x", nil); ok {
		t.Error("suggested something from an empty list")
	}
}
