package exttool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type outcome struct {
	fatal, skip, logged []string
}

func (o *outcome) Helper()                   {}
func (o *outcome) Fatalf(f string, a ...any) { o.fatal = append(o.fatal, fmt.Sprintf(f, a...)) }
func (o *outcome) Skipf(f string, a ...any)  { o.skip = append(o.skip, fmt.Sprintf(f, a...)) }
func (o *outcome) Logf(f string, a ...any)   { o.logged = append(o.logged, fmt.Sprintf(f, a...)) }

func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func clean(t *testing.T) {
	t.Helper()
	t.Setenv(PythonVar, "")
	t.Setenv(RequirePythonVar, "")
	t.Setenv(RequireB3SumVar, "")
}

func TestPythonThatWorksIsReturned(t *testing.T) {
	clean(t)
	exe := script(t, "exit 0")
	t.Setenv(PythonVar, exe)
	var o outcome
	if got := Python(&o); got != exe || len(o.fatal)+len(o.skip) != 0 {
		t.Fatalf("got %q, %+v", got, o)
	}
}

func TestPythonNamedExplicitlyButBrokenFails(t *testing.T) {
	clean(t)
	t.Setenv(PythonVar, filepath.Join(t.TempDir(), "no-such-python"))
	var o outcome
	Python(&o)
	if len(o.fatal) != 1 || len(o.skip) != 0 {
		t.Fatalf("a variable that was set and is broken must fail, not skip: %+v", o)
	}
}

func TestPythonWithoutBlake3NamedExplicitlyFails(t *testing.T) {
	clean(t)
	t.Setenv(PythonVar, script(t, "exit 1"))
	var o outcome
	Python(&o)
	if len(o.fatal) != 1 {
		t.Fatalf("%+v", o)
	}
}

func TestPythonMissingAndNotRequiredSkipsWithInstructions(t *testing.T) {
	clean(t)
	t.Setenv("PATH", t.TempDir())
	var o outcome
	Python(&o)
	if len(o.skip) != 1 || len(o.fatal) != 0 {
		t.Fatalf("%+v", o)
	}
	if !strings.Contains(o.skip[0], RequirePythonVar) || !strings.Contains(o.skip[0], PythonVar) {
		t.Fatalf("the skip must say how to turn it into a failure: %s", o.skip[0])
	}
}

func TestPythonMissingAndRequiredFails(t *testing.T) {
	clean(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv(RequirePythonVar, "1")
	var o outcome
	Python(&o)
	if len(o.fatal) != 1 || len(o.skip) != 0 {
		t.Fatalf("%+v", o)
	}
}

func TestB3SumFoundIsUsed(t *testing.T) {
	clean(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b3sum"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	var o outcome
	path, ok := B3Sum(&o)
	if !ok || filepath.Base(path) != "b3sum" || len(o.fatal)+len(o.logged) != 0 {
		t.Fatalf("%q %v %+v", path, ok, o)
	}
}

func TestB3SumAbsentIsLoudNotSilent(t *testing.T) {
	clean(t)
	t.Setenv("PATH", t.TempDir())
	var o outcome
	if _, ok := B3Sum(&o); ok || len(o.logged) != 1 || len(o.fatal) != 0 {
		t.Fatalf("%+v", o)
	}
}

func TestB3SumAbsentAndRequiredFails(t *testing.T) {
	clean(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv(RequireB3SumVar, "1")
	var o outcome
	if _, ok := B3Sum(&o); ok || len(o.fatal) != 1 {
		t.Fatalf("%+v", o)
	}
}
