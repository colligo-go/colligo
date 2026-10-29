// Package exttool finds the external programs some tests compare against.
// A missing program may skip a test only when nobody asked for it: if the
// variable naming it is set, or the matching COLLIGO_REQUIRE_* switch is on,
// the test fails instead, so a green suite never hides a check that did not run.
package exttool

import (
	"os"
	"os/exec"
)

const (
	PythonVar        = "COLLIGO_PYTHON"
	RequirePythonVar = "COLLIGO_REQUIRE_PYTHON"
	RequireB3SumVar  = "COLLIGO_REQUIRE_B3SUM"
)

type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Skipf(format string, args ...any)
	Logf(format string, args ...any)
}

func on(variable string) bool { return os.Getenv(variable) != "" }

func Python(t TB) string {
	t.Helper()
	exe := os.Getenv(PythonVar)
	asked := exe != ""
	if !asked {
		exe = "python3"
	}
	err := exec.Command(exe, "-c", "import blake3").Run()
	if err == nil {
		return exe
	}
	if asked || on(RequirePythonVar) {
		t.Fatalf("the independent verifier cannot run: %s with the blake3 package is required (%s=%q, %s=%q): %v",
			exe, PythonVar, os.Getenv(PythonVar), RequirePythonVar, os.Getenv(RequirePythonVar), err)
		return ""
	}
	t.Skipf("%s with the blake3 package is needed for the independent verifier: set %s, or %s=1 to make this a failure: %v",
		exe, PythonVar, RequirePythonVar, err)
	return ""
}

func B3Sum(t TB) (string, bool) {
	t.Helper()
	path, err := exec.LookPath("b3sum")
	if err == nil {
		return path, true
	}
	if on(RequireB3SumVar) {
		t.Fatalf("b3sum is required (%s is set) but was not found: %v", RequireB3SumVar, err)
		return "", false
	}
	t.Logf("b3sum not found: the comparison against b3sum was NOT run; set %s=1 to make this a failure", RequireB3SumVar)
	return "", false
}
