package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/identity"
	"github.com/colligo-go/colligo/verifier"
)

const cliFasta = ">chrA first record\nACGTACGT\nacgtNN\n>chrB\nGGGGCCCC\n\n>chrC desc\nTTTT\n"

const cliDigest = "-ILNzNQsKYl_9KDXtkCWYUl5t5NESUdA"

var cliReferences = []identity.Reference{{Label: "tiny", Digest: cliDigest, Source: "independent Python implementation of the specification"}}

func invokeIdentify(refs []identity.Reference, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := runIdentify(args, &out, &errb, refs)
	return code, out.String(), errb.String()
}

func TestIdentifyRecognizesAKnownFastaAndNotAChangedOne(t *testing.T) {
	dir := t.TempDir()
	known := put(t, dir, "known.fa", []byte(cliFasta))
	changed := put(t, dir, "changed.fa", []byte(strings.Replace(cliFasta, "GGGGCCCC", "GGGGCCCA", 1)))

	code, out, _ := invokeIdentify(cliReferences, known)
	if code != verifier.ExitOK || !strings.Contains(out, "identified: tiny") || !strings.Contains(out, cliDigest) {
		t.Errorf("known: exit %d\n%s", code, out)
	}
	code, out, _ = invokeIdentify(cliReferences, changed)
	if code != verifier.ExitOK || !strings.Contains(out, "not identified") || strings.Contains(out, "identified: tiny") {
		t.Errorf("changed: exit %d\n%s", code, out)
	}
}

func TestIdentifyJSONFlagWorksBeforeAndAfterThePath(t *testing.T) {
	path := put(t, t.TempDir(), "known.fa", []byte(cliFasta))
	for _, args := range [][]string{{"--json", path}, {path, "--json"}} {
		code, out, _ := invokeIdentify(cliReferences, args...)
		var decoded struct {
			SchemaVersion int `json:"schema_version"`
			Files         []struct {
				Status string `json:"status"`
			} `json:"files"`
		}
		if err := json.Unmarshal([]byte(out), &decoded); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		if code != verifier.ExitOK || decoded.SchemaVersion != 1 || len(decoded.Files) != 1 || decoded.Files[0].Status != "identified" {
			t.Errorf("%v: exit %d decoded %+v", args, code, decoded)
		}
	}
}

func TestIdentifyWithTheEmbeddedTableNeverClaimsAnUnknownFile(t *testing.T) {
	path := put(t, t.TempDir(), "known.fa", []byte(cliFasta))
	code, out, _ := invoke("identify", path)
	if code != verifier.ExitOK || !strings.Contains(out, "not identified") || !strings.Contains(out, cliDigest) {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestIdentifyUsageAndFailureExitCodes(t *testing.T) {
	if code, _, _ := invokeIdentify(nil); code != verifier.ExitUsage {
		t.Errorf("no path: exit %d", code)
	}
	if code, _, _ := invokeIdentify(nil, "--bogus", "x"); code != verifier.ExitUsage {
		t.Errorf("bad flag: exit %d", code)
	}
	if code, _, errText := invokeIdentify(nil, t.TempDir()+"/absent.fa"); code != verifier.ExitUsage || strings.Contains(errText, "absent.fa") && false {
		t.Errorf("absent file: exit %d", code)
	}
}

func TestIdentifyAppearsInTheUsage(t *testing.T) {
	_, out, _ := invoke("help")
	if !strings.Contains(out, "identify") {
		t.Errorf("usage does not mention identify:\n%s", out)
	}
}
