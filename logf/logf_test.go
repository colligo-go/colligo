package logf

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/secret"
)

func TestEndpointFieldNeverLeaksTheQuery(t *testing.T) {
	const canary = "CANARY-MUST-NOT-LEAK"
	ep, err := secret.NewEndpoint("https://example.org/download?token=" + canary)
	if err != nil {
		t.Fatal(err)
	}
	f := Endpoint("url", ep)

	formatted := fmt.Sprintf("%v %s %+v %#v", f.Value(), f.Value(), f.Value(), f.Value())
	if strings.Contains(formatted, canary) {
		t.Errorf("field leaked the canary when formatted: %q", formatted)
	}

	b, err := json.Marshal(f.Value())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), canary) {
		t.Errorf("field leaked the canary in JSON: %q", b)
	}
}

func TestStrAndErrKeepTheirValue(t *testing.T) {
	f := Str("format", "fastq")
	if f.Label() != "format" || f.Value() != "fastq" {
		t.Errorf("Str lost its label or value: %+v", f)
	}

	err := fmt.Errorf("network failure")
	ef := Err(err)
	if ef.Label() != "error" || ef.Value() != err {
		t.Errorf("Err lost the error: %+v", ef)
	}
}
