package checksum

import (
	"errors"
	"testing"

	"github.com/colligo-go/colligo/model"
)

func TestVerifyPassesWithThePublishedVectors(t *testing.T) {
	if err := Verify(); err != nil {
		t.Fatalf("Verify() = %v, want nil", err)
	}
}

func TestVerifyDetectsSabotage(t *testing.T) {
	original := vectors
	t.Cleanup(func() { vectors = original })

	sabotaged := make([]vector, len(original))
	copy(sabotaged, original)
	sabotaged[0].want = "0000000000000000000000000000000000000000000000000000000000000000"
	vectors = sabotaged

	err := Verify()
	if err == nil {
		t.Fatal("Verify() did not detect the sabotaged vector")
	}
	var selfTest *SelfTestError
	if !errors.As(err, &selfTest) {
		t.Fatalf("error is not a *SelfTestError: %v (%T)", err, err)
	}
	if selfTest.Severity() != model.Fatal {
		t.Errorf("Severity() = %q, want %q", selfTest.Severity(), model.Fatal)
	}
}
