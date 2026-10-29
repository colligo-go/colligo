package fsaccess

import "testing"

func TestRevealChamaFnComUmFSAccess(t *testing.T) {
	called := false
	got := Reveal(func(fs FSAccess) string {
		called = true
		return "valor"
	})
	if !called {
		t.Fatal("Reveal não chamou fn")
	}
	if got != "valor" {
		t.Errorf("Reveal() = %q, esperado %q", got, "valor")
	}
}
