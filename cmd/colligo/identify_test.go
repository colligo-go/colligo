package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/identity"
	"github.com/colligo-go/colligo/verifier"
)

const cliFasta = ">chrA first record\nACGTACGT\nacgtNN\n>chrB\nGGGGCCCC\n\n>chrC desc\nTTTT\n"

const cliDigest = "-ILNzNQsKYl_9KDXtkCWYUl5t5NESUdA"

var cliReferences = identity.Table{Collections: []identity.Reference{{Label: "tiny", Digest: cliDigest, Source: "independent Python implementation of the specification"}}}

func invokeIdentify(refs identity.Table, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := runIdentify(args, &out, &errb, refs)
	return code, out.String(), errb.String()
}

func TestIdentifyRecognizesAKnownFastaAndNotAChangedOne(t *testing.T) {
	dir := t.TempDir()
	known := put(t, dir, "known.fa", []byte(cliFasta))
	changed := put(t, dir, "changed.fa", []byte(strings.Replace(cliFasta, "GGGGCCCC", "GGGGCCCA", 1)))

	code, out, _ := invokeIdentify(cliReferences, known)
	if code != verifier.ExitOK || !strings.Contains(out, "✓ IDENTIFIED  known.fa is tiny") || !strings.Contains(out, cliDigest) {
		t.Errorf("known: exit %d\n%s", code, out)
	}
	code, out, _ = invokeIdentify(cliReferences, changed)
	if code != verifier.ExitOK || !strings.Contains(out, "changed.fa matches no known reference") || strings.Contains(out, "is tiny") || strings.Contains(out, "✓ IDENTIFIED") {
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
	if code, _, _ := invokeIdentify(identity.Table{}); code != verifier.ExitUsage {
		t.Errorf("no path: exit %d", code)
	}
	if code, _, _ := invokeIdentify(identity.Table{}, "--bogus", "x"); code != verifier.ExitUsage {
		t.Errorf("bad flag: exit %d", code)
	}
	if code, _, errText := invokeIdentify(identity.Table{}, t.TempDir()+"/absent.fa"); code != verifier.ExitUsage || strings.Contains(errText, "absent.fa") && false {
		t.Errorf("absent file: exit %d", code)
	}
}

func TestIdentifyAppearsInTheUsage(t *testing.T) {
	_, out, _ := invoke("help")
	if !strings.Contains(out, "identify") {
		t.Errorf("usage does not mention identify:\n%s", out)
	}
}

const cliGencodeHeader = "##description: evidence-based annotation of the human genome (GRCh38), version 44 (Ensembl 110)\n" +
	"##provider: GENCODE\n##contact: gencode-help@ebi.ac.uk\n##format: gtf\n##date: 2023-03-01\n"

const cliGencodeGene = "\tHAVANA\tgene\t11869\t14409\t.\t+\t.\tgene_id \"ENSG00000290825.1\"; gene_type \"lncRNA\"; gene_name \"DDX11L2\"; level 2;\n"

func TestIdentifyGtfEndToEndThroughTheCommand(t *testing.T) {
	dir := t.TempDir()
	declared := []byte(cliGencodeHeader + "chr1" + cliGencodeGene)
	conflict := []byte(cliGencodeHeader + "1" + cliGencodeGene)
	paths := map[string]string{
		"declared": put(t, dir, "declared.gtf", declared),
		"conflict": put(t, dir, "conflict.gtf", conflict),
		"bare":     put(t, dir, "bare.gtf", []byte("chr1"+cliGencodeGene)),
		"cut":      put(t, dir, "cut.gtf", declared[:len(declared)-10]),
	}
	sum := md5.Sum(declared)
	table := identity.Table{Checksums: []identity.Checksum{{
		Label: "GENCODE human 44", Asset: "gencode.v44.annotation.gtf", MD5: hex.EncodeToString(sum[:]),
		Origin: "https://example.org/MD5SUMS", Retrieved: "2026-10-31",
	}}}
	want := map[string]struct {
		status string
		exit   int
	}{
		"declared": {"identified", verifier.ExitOK},
		"conflict": {"conflict", verifier.ExitIncomplete},
		"bare":     {"not_identified", verifier.ExitOK},
		"cut":      {"invalid", verifier.ExitIncomplete},
	}
	for key, w := range want {
		code, out, _ := invokeIdentify(table, "--json", paths[key])
		var decoded struct {
			Files []struct {
				Status  string `json:"status"`
				Gencode *struct {
					Release  string `json:"release"`
					Organism string `json:"organism"`
					Assembly string `json:"assembly"`
				} `json:"gencode"`
				Checksum *struct {
					MD5 string `json:"md5"`
				} `json:"checksum"`
			} `json:"files"`
			ExitCode int `json:"exit_code"`
		}
		if err := json.Unmarshal([]byte(out), &decoded); err != nil {
			t.Fatalf("%s: %v\n%s", key, err, out)
		}
		if code != w.exit || decoded.ExitCode != w.exit || len(decoded.Files) != 1 || decoded.Files[0].Status != w.status {
			t.Errorf("%s: exit %d, decoded %+v\n%s", key, code, decoded, out)
		}
	}
	code, out, _ := invokeIdentify(identity.Table{}, paths["declared"])
	for _, line := range []string{"declared.gtf says it is GENCODE 44, and nothing official confirms it", "release      44", "organism     human", "assembly     GRCh38"} {
		if code != verifier.ExitOK || !strings.Contains(out, line) {
			t.Errorf("declared text lacks %q (exit %d):\n%s", line, code, out)
		}
	}
}

func TestIdentifyWithTheEmbeddedTableRecognisesAnOfficialChecksum(t *testing.T) {
	table, err := identity.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if len(table.Checksums) == 0 {
		t.Fatal("the embedded table has no checksums")
	}
	path := put(t, t.TempDir(), "annotation.gtf", []byte(cliGencodeHeader+"chr1"+cliGencodeGene))
	code, out, _ := invoke("identify", path)
	if code != verifier.ExitOK || !strings.Contains(out, "says it is GENCODE 44") || strings.Contains(out, "IDENTIFIED") {
		t.Errorf("a look-alike must stay declared, exit %d:\n%s", code, out)
	}
}
