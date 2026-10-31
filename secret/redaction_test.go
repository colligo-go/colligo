package secret_test

import (
	"encoding/json"
	"fmt"
	"go/types"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/tools/go/packages"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/facts"
	"github.com/colligo-go/colligo/logf"
	"github.com/colligo-go/colligo/manifest"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/verifier"
)

const sentinel = "SENTINEL-RESTRICTED-7f3a"

var domainTypes = map[string]reflect.Type{
	"secret.Name":             reflect.TypeOf(secret.Name{}),
	"secret.Endpoint":         reflect.TypeOf(secret.Endpoint{}),
	"secret.Secret":           reflect.TypeOf(secret.Secret{}),
	"logf.Field":              reflect.TypeOf(logf.Field{}),
	"model.Fact":              reflect.TypeOf(model.Fact{}),
	"model.Assertion":         reflect.TypeOf(model.Assertion{}),
	"model.Severity":          reflect.TypeOf(model.Severity("")),
	"facts.Options":           reflect.TypeOf(facts.Options{}),
	"facts.Result":            reflect.TypeOf(facts.Result{}),
	"verifier.Expected":       reflect.TypeOf(verifier.Expected{}),
	"verifier.Options":        reflect.TypeOf(verifier.Options{}),
	"verifier.FileReport":     reflect.TypeOf(verifier.FileReport{}),
	"verifier.Report":         reflect.TypeOf(verifier.Report{}),
	"verifier.Identification": reflect.TypeOf(verifier.Identification{}),
	"verifier.IdentifyReport": reflect.TypeOf(verifier.IdentifyReport{}),
	"manifest.Tool":           reflect.TypeOf(manifest.Tool{}),
	"manifest.Certificate":    reflect.TypeOf(manifest.Certificate{}),
	"diagnoses.Entry":         reflect.TypeOf(diagnoses.Entry{}),
	"diagnoses.Values":        reflect.TypeOf(diagnoses.Values{}),
}

func TestEveryExportedDomainTypeIsCovered(t *testing.T) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes, Dir: ".."}
	pkgs, err := packages.Load(cfg, "./secret", "./logf", "./model", "./facts", "./verifier", "./manifest", "./diagnoses")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		scope := p.Types.Scope()
		for _, n := range scope.Names() {
			tn, ok := scope.Lookup(n).(*types.TypeName)
			if !ok || !tn.Exported() {
				continue
			}
			key := p.Name + "." + n
			if _, ok := domainTypes[key]; !ok {
				t.Errorf("%s is exported but not in the redaction test: add it to domainTypes", key)
			}
		}
	}
}

func settable(v reflect.Value) reflect.Value {
	if v.CanSet() {
		return v
	}
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
}

var (
	nameType     = reflect.TypeOf(secret.Name{})
	endpointType = reflect.TypeOf(secret.Endpoint{})
	secretType   = reflect.TypeOf(secret.Secret{})
)

func plant(v reflect.Value, depth int) {
	if depth > 5 {
		return
	}
	v = settable(v)
	switch v.Type() {
	case nameType:
		v.Set(reflect.ValueOf(secret.RestrictedNameForTest("/clinic/"+sentinel+".fastq.gz", "0a1b2c3d4e5f6a7b")))
		return
	case endpointType:
		v.Set(reflect.ValueOf(secret.RestrictedEndpointForTest("https://dbgap.example/" + sentinel + "?token=" + sentinel)))
		return
	case secretType:
		v.Set(reflect.ValueOf(secret.SecretForTest(sentinel)))
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			plant(v.Field(i), depth+1)
		}
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		plant(v.Elem(), depth+1)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		s := reflect.MakeSlice(v.Type(), 1, 1)
		plant(s.Index(0), depth+1)
		v.Set(s)
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf(secret.RestrictedNameForTest("/clinic/"+sentinel, "0a1b2c3d4e5f6a7b")))
		}
	}
}

func TestNoExportedTypeLeaksASecretValue(t *testing.T) {
	for key, typ := range domainTypes {
		ptr := reflect.New(typ)
		plant(ptr.Elem(), 0)
		for _, operand := range []any{ptr.Elem().Interface(), ptr.Interface()} {
			for _, verb := range []string{"%v", "%s", "%+v", "%#v"} {
				if out := fmt.Sprintf(verb, operand); strings.Contains(out, sentinel) {
					t.Errorf("%s with %s leaks: %s", key, verb, out)
				}
			}
			if out, err := json.Marshal(operand); err == nil && strings.Contains(string(out), sentinel) {
				t.Errorf("%s with json.Marshal leaks: %s", key, out)
			}
		}
	}
}
