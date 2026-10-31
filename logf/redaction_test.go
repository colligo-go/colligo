package logf

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/colligo-go/colligo/secret"
)

const redactionCanary = "CANARY-MUST-NOT-LEAK"

func planted[T any](t *testing.T, fields map[string]any) T {
	t.Helper()
	var v T
	rv := reflect.ValueOf(&v).Elem()
	for name, value := range fields {
		f := rv.FieldByName(name)
		if !f.IsValid() {
			t.Fatalf("%T has no field %s", v, name)
		}
		reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
	}
	return v
}

func requireNoCanary(t *testing.T, f Field) {
	t.Helper()
	rendered := map[string]string{
		"%v":             fmt.Sprintf("%v", f),
		"%s":             fmt.Sprintf("%s", f),
		"%+v":            fmt.Sprintf("%+v", f),
		"%#v":            fmt.Sprintf("%#v", f),
		"%v of Value()":  fmt.Sprintf("%v", f.Value()),
		"%+v of Value()": fmt.Sprintf("%+v", f.Value()),
		"%#v of Value()": fmt.Sprintf("%#v", f.Value()),
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	rendered["json"] = string(b)
	for how, out := range rendered {
		if strings.Contains(out, redactionCanary) {
			t.Errorf("%s leaked the canary: %q", how, out)
		}
	}
}

func TestNameFieldNeverLeaksARestrictedPath(t *testing.T) {
	f := Name("file", planted[secret.Name](t, map[string]any{"raw": "/data/" + redactionCanary + "/sample.fastq", "opaque": "restricted-file", "restricted": true}))
	requireNoCanary(t, f)
	if got := f.String(); got != "file=restricted-file" {
		t.Errorf("String() = %q", got)
	}
	if f.Label() != "file" {
		t.Errorf("label %q", f.Label())
	}
}

func TestSecretFieldNeverLeaksTheSecret(t *testing.T) {
	f := Secret("token", planted[secret.Secret](t, map[string]any{"v": redactionCanary}))
	requireNoCanary(t, f)
	if got := f.String(); got != "token=[REDACTED]" {
		t.Errorf("String() = %q", got)
	}
}

func TestEndpointFieldStringKeepsLabelAndHidesTheQuery(t *testing.T) {
	ep, err := secret.NewEndpoint("https://example.org/a?token=" + redactionCanary)
	if err != nil {
		t.Fatal(err)
	}
	f := Endpoint("url", ep)
	requireNoCanary(t, f)
	if got := f.String(); got != "url=https://example.org/a" {
		t.Errorf("String() = %q", got)
	}
}
