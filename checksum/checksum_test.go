package checksum

import (
	"errors"
	"testing"

	"github.com/colligo-go/colligo/model"
)

func TestVerifyPassaComOsVetoresReais(t *testing.T) {
	if err := Verify(); err != nil {
		t.Fatalf("Verify() = %v, esperado nil", err)
	}
}

func TestVerifyDetectaSabotagem(t *testing.T) {
	original := vectors
	t.Cleanup(func() { vectors = original })

	sabotado := make([]vector, len(original))
	copy(sabotado, original)
	sabotado[0].want = "0000000000000000000000000000000000000000000000000000000000000000"
	vectors = sabotado

	err := Verify()
	if err == nil {
		t.Fatal("Verify() não detectou o vetor sabotado")
	}
	var selfTest *SelfTestError
	if !errors.As(err, &selfTest) {
		t.Fatalf("erro não é *SelfTestError: %v (%T)", err, err)
	}
	if selfTest.Severity() != model.Fatal {
		t.Errorf("Severity() = %q, want %q", selfTest.Severity(), model.Fatal)
	}
}
