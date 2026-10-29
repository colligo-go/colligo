package logf

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/secret"
)

func TestEndpointFieldNeverLeaksAQueryNoFormatoNemNoJSON(t *testing.T) {
	const canary = "CANARY-SEGREDO-NAO-PODE-VAZAR"
	ep, err := secret.NewEndpoint("https://exemplo.org/download?token=" + canary)
	if err != nil {
		t.Fatal(err)
	}
	f := Endpoint("url", ep)

	formatted := fmt.Sprintf("%v %s %+v %#v", f.Value(), f.Value(), f.Value(), f.Value())
	if strings.Contains(formatted, canary) {
		t.Errorf("campo vazou a sentinela em texto formatado: %q", formatted)
	}

	b, err := json.Marshal(f.Value())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), canary) {
		t.Errorf("campo vazou a sentinela em JSON: %q", b)
	}
}

func TestStrEErrGuardamOValorSemRedacao(t *testing.T) {
	f := Str("formato", "fastq")
	if f.Label() != "formato" || f.Value() != "fastq" {
		t.Errorf("Str não preservou rótulo/valor: %+v", f)
	}

	err := fmt.Errorf("falha de rede")
	ef := Err(err)
	if ef.Label() != "error" || ef.Value() != err {
		t.Errorf("Err não preservou o erro: %+v", ef)
	}
}
