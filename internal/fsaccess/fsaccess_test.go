package fsaccess

import "testing"

func TestRevealCallsFnWithAnFSAccess(t *testing.T) {
	called := false
	got := Reveal(func(fs FSAccess) string {
		called = true
		return "value"
	})
	if !called {
		t.Fatal("Reveal did not call fn")
	}
	if got != "value" {
		t.Errorf("Reveal() = %q, want %q", got, "value")
	}
}
