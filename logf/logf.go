// Package logf defines typed log fields. There is no generic string field
// for a path, URL or credential: those three require the secret/ types,
// structurally, in the signature.
package logf

import (
	"encoding/json"
	"fmt"

	"github.com/colligo-go/colligo/secret"
)

type Field struct {
	label string
	value any
}

func Name(label string, v secret.Name) Field         { return Field{label: label, value: v} }
func Endpoint(label string, v secret.Endpoint) Field { return Field{label: label, value: v} }
func Secret(label string, v secret.Secret) Field     { return Field{label: label, value: v} }

func Str(label, value string) Field { return Field{label: label, value: value} }

func Err(err error) Field { return Field{label: "error", value: err} }

func (f Field) Label() string { return f.label }

func (f Field) Value() any { return f.value }

// String, GoString and MarshalJSON format the value through its own methods.
// Without them fmt would print the unexported field raw, bypassing the
// redaction of the secret/ types.
func (f Field) String() string   { return f.label + "=" + fmt.Sprint(f.value) }
func (f Field) GoString() string { return f.String() }

func (f Field) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Label string `json:"label"`
		Value any    `json:"value"`
	}{f.label, f.value})
}
