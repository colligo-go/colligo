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

func TestResolvePlacesAPathNextToTheNamedFile(t *testing.T) {
	name := func(FSAccess) string { return "study/inputs/samples.csv" }
	if got, want := Resolve(name, "reads/r1.fq"), "study/inputs/reads/r1.fq"; got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
	bare := func(FSAccess) string { return "samples.csv" }
	if got, want := Resolve(bare, "r1.fq"), "r1.fq"; got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

func TestSamePathIgnoresRedundantElements(t *testing.T) {
	a := func(FSAccess) string { return "dir/./sub/../r1.fq" }
	b := func(FSAccess) string { return "dir/r1.fq" }
	c := func(FSAccess) string { return "dir/r2.fq" }
	if !SamePath(a, b) || SamePath(a, c) {
		t.Fatal("SamePath must compare cleaned paths")
	}
}
