package secret

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/internal/fsaccess"
)

const canary = "CANARY-SEGREDO-NAO-PODE-VAZAR"

func assertNeverLeaks(t *testing.T, label string, v any) {
	t.Helper()
	forms := []string{
		fmt.Sprintf("%v", v),
		fmt.Sprintf("%s", v),
		fmt.Sprintf("%+v", v),
		fmt.Sprintf("%#v", v),
	}
	if b, err := json.Marshal(v); err != nil {
		t.Fatalf("%s: json.Marshal: %v", label, err)
	} else {
		forms = append(forms, string(b))
	}
	for _, f := range forms {
		if strings.Contains(f, canary) {
			t.Errorf("%s: vazou a sentinela em %q", label, f)
		}
	}
}

func TestSecretNeverLeaks(t *testing.T) {
	assertNeverLeaks(t, "Secret", Secret{v: canary})
}

func TestNamePublicNeverLeaksQuandoRestrito(t *testing.T) {
	assertNeverLeaks(t, "Name restrito", Name{raw: canary, opaque: "op4c0id", restricted: true})
}

func TestNamePublicoMostraORaw(t *testing.T) {
	n := NewName("arquivo.fastq.gz")
	if got := n.String(); got != "arquivo.fastq.gz" {
		t.Errorf("String() = %q, esperado %q", got, "arquivo.fastq.gz")
	}
}

func TestNameRaw(t *testing.T) {
	n := Name{raw: canary, restricted: true, opaque: "op4c0id"}
	if got := fsaccess.Reveal(n.Raw); got != canary {
		t.Errorf("Raw() = %q, esperado o valor real", got)
	}
}

func TestEndpointNeverLeaksQuery(t *testing.T) {
	e, err := NewEndpoint("https://exemplo.org/path?token=" + canary)
	if err != nil {
		t.Fatal(err)
	}
	assertNeverLeaks(t, "Endpoint com query", e)
}

func TestEndpointRestritoNeverLeaksHost(t *testing.T) {
	e, err := NewEndpoint("https://exemplo.org/" + canary + "?x=1")
	if err != nil {
		t.Fatal(err)
	}
	e.restricted = true
	assertNeverLeaks(t, "Endpoint restrito", e)
	if got := e.String(); got != "https://exemplo.org/[REDACTED]" {
		t.Errorf("String() restrito = %q", got)
	}
}
