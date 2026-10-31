package json_test

import (
	stdjson "encoding/json"
	"strings"
	"testing"

	renderjson "github.com/colligo-go/colligo/render/json"
	"github.com/colligo-go/colligo/verifier"
)

func TestCheckWritesTheSchemaAndTheExitCode(t *testing.T) {
	var out strings.Builder
	if err := renderjson.Check(&out, verifier.Report{Files: []*verifier.FileReport{}}, 3); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		SchemaVersion int   `json:"schema_version"`
		ExitCode      int   `json:"exit_code"`
		Files         []any `json:"files"`
	}
	if err := stdjson.Unmarshal([]byte(out.String()), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.SchemaVersion != verifier.SchemaVersion || doc.ExitCode != 3 || doc.Files == nil {
		t.Fatalf("%+v", doc)
	}
}

func TestPresentationFieldsNeverReachTheDocument(t *testing.T) {
	var out strings.Builder
	report := verifier.Report{Files: []*verifier.FileReport{}, Quick: true, FromSamplesheet: true, Elapsed: 5}
	if err := renderjson.Check(&out, report, 0); err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"Elapsed", "elapsed", "Quick", "quick", "FromSamplesheet", "samplesheet"} {
		if strings.Contains(out.String(), leaked) {
			t.Errorf("%q reached the JSON document:\n%s", leaked, out.String())
		}
	}
}

func TestIdentifyWithoutFilesIsAnEmptyListNotNull(t *testing.T) {
	var out strings.Builder
	if err := renderjson.Identify(&out, verifier.IdentifyReport{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"files": []`) || !strings.Contains(out.String(), `"exit_code": 3`) {
		t.Fatalf("%s", out.String())
	}
}
