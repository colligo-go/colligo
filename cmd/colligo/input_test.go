package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/transport"
	"github.com/colligo-go/colligo/verifier"
)

type recordingTransport struct {
	fakeTransport
	asked []string
}

func (r *recordingTransport) Get(ctx context.Context, e secret.Endpoint, b *transport.ByteRange) (*transport.Response, error) {
	r.asked = append(r.asked, e.Reveal())
	return r.fakeTransport.Get(ctx, e, b)
}

func oneRunFolder(t *testing.T, accession string) (dir string, net *recordingTransport) {
	t.Helper()
	dir = t.TempDir()
	writeDecompressed(t, dir, syntheticRun(specOf(t, accession)))
	return dir, &recordingTransport{fakeTransport: fakeTransport{body: recorded(t, accession)}}
}

func checkWithStdin(t *testing.T, stdin string, net transport.Transport, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runCheckWith(args, &out, &errb, false, net, strings.NewReader(stdin))
	return code, out.String(), errb.String()
}

func TestSourceLinkIsAcceptedAndTheAccessionTakenFromItIsShown(t *testing.T) {
	dir, net := oneRunFolder(t, "DRR021374")
	link := "https://www.ebi.ac.uk/ena/browser/view/DRR021374"
	code, out, errText := checkSource(t, net, "--source", link, "--color=never", dir)
	if code != verifier.ExitOK {
		t.Fatalf("exit %d\n%s\n%s", code, out, errText)
	}
	if !strings.Contains(out, "Input: accession DRR021374, taken from the link") {
		t.Errorf("the interpretation is not in the report:\n%s", out)
	}
	if len(net.asked) != 1 || !strings.Contains(net.asked[0], "DRR021374") {
		t.Errorf("the source was asked %v, want one query for DRR021374", net.asked)
	}
	_, js, _ := checkSource(t, net, "--source", link, "--json", dir)
	var doc struct {
		Source struct {
			Accession     string `json:"accession"`
			AccessionFrom string `json:"accession_from"`
		} `json:"source"`
	}
	if err := json.Unmarshal([]byte(js), &doc); err != nil || doc.Source.Accession != "DRR021374" || doc.Source.AccessionFrom != "link" {
		t.Errorf("json source %+v (%v)", doc.Source, err)
	}
}

func TestSourceTypedInLowerCaseIsUpperCasedAndSaid(t *testing.T) {
	dir, net := oneRunFolder(t, "DRR021374")
	code, out, _ := checkSource(t, net, "--source", " ena:drr021374 ", "--color=never", dir)
	if code != verifier.ExitOK || !strings.Contains(out, "Input: accession DRR021374, upper-cased from what was typed") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestAnAccessionTypedAsIsNeedsNoExplanation(t *testing.T) {
	dir, net := oneRunFolder(t, "DRR021374")
	_, out, _ := checkSource(t, net, "--source", "ena:DRR021374", "--color=never", dir)
	if strings.Contains(out, "Input:") {
		t.Errorf("an accession typed exactly was explained:\n%s", out)
	}
	_, js, _ := checkSource(t, net, "--source", "ena:DRR021374", "--json", dir)
	if strings.Contains(js, "accession_from") {
		t.Errorf("accession_from appears for an accession typed as is:\n%s", js)
	}
}

func TestEnaAloneDeducesTheAccessionFromTheFileNames(t *testing.T) {
	dir, net := oneRunFolder(t, "DRR021374")
	code, out, errText := checkSource(t, net, "--source", "ena", "--color=never", dir)
	if code != verifier.ExitOK {
		t.Fatalf("exit %d\n%s\n%s", code, out, errText)
	}
	if !strings.Contains(out, "Input: accession DRR021374, deduced from the file names") {
		t.Errorf("the deduction is not in the report:\n%s", out)
	}
	_, quiet, _ := checkSource(t, net, "--source", "ena", "--quiet", "--color=never", dir)
	if !strings.Contains(quiet, "deduced from the file names") {
		t.Errorf("--quiet hid the interpretation:\n%s", quiet)
	}
}

func TestEnaAloneOverTwoRunsIsAUsageErrorThatListsBothAndAsksNothingOfTheNetwork(t *testing.T) {
	dir := t.TempDir()
	writeDecompressed(t, dir, syntheticRun(specOf(t, "DRR021374")))
	writeDecompressed(t, dir, syntheticRun(specOf(t, "ERR12477684")))
	net := &recordingTransport{fakeTransport: fakeTransport{body: recorded(t, "DRR021374")}}
	code, out, errText := checkSource(t, net, "--source", "ena", dir)
	if code != verifier.ExitUsage || out != "" {
		t.Fatalf("exit %d stdout %q", code, out)
	}
	if !strings.Contains(errText, "DRR021374") || !strings.Contains(errText, "ERR12477684") {
		t.Errorf("stderr does not list both runs:\n%s", errText)
	}
	if len(net.asked) != 0 {
		t.Errorf("the network was asked before the usage error: %v", net.asked)
	}
}

func TestNoSourceMistakeEverReachesTheNetwork(t *testing.T) {
	dir, net := oneRunFolder(t, "DRR021374")
	for _, value := range []string{"https://www.ncbi.nlm.nih.gov/sra/SRR1", "geo:GSE1", "ena:", "https://www.ebi.ac.uk/ena/browser/home", "ena:DRR021374 extra:more"} {
		if code, _, _ := checkSource(t, net, "--source", value, dir); code != verifier.ExitUsage {
			t.Errorf("%q: exit %d, want usage", value, code)
		}
	}
	if len(net.asked) != 0 {
		t.Errorf("a malformed --source reached the network: %v", net.asked)
	}
	if code, _, _ := checkWithStdin(t, "", net, dir); code != verifier.ExitOK || len(net.asked) != 0 {
		t.Errorf("a plain check asked the network: %v", net.asked)
	}
}

func TestFilesFromNamesTheMissingLineAndStillChecksTheOthers(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "a_R1.fastq", fastq("S", 10))
	put(t, dir, "a_R2.fastq", fastq("S", 10))
	list := put(t, dir, "files.txt", []byte("# my run\na_R1.fastq\ngone.fastq\n\na_R2.fastq\n"))
	code, out, _ := checkSource(t, nil, "--files-from", list, "--color=never")
	if code != verifier.ExitFatal {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, want := range []string{"read_failed", "list line 3", "3 files: 2 verified, 1 unreadable"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestFilesFromStandardInputIsRelativeToTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "a.fastq", fastq("S", 5))
	t.Chdir(dir)
	code, out, errText := checkWithStdin(t, "a.fastq\n", nil, "--files-from", "-", "--color=never")
	if code != verifier.ExitOK || !strings.Contains(out, "1 file passes every check run") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errText)
	}
}

func TestFilesFromReadsAPathListedTwiceOnce(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "a.fastq", fastq("S", 5))
	list := put(t, dir, "files.txt", []byte("a.fastq\na.fastq\n"))
	_, out, _ := checkSource(t, nil, "--files-from", list, "--json")
	var doc struct {
		Files []json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Files) != 1 {
		t.Errorf("%d files (%v)", len(doc.Files), err)
	}
}

func TestACheckWithNoPathSuggestsTheCurrentFolder(t *testing.T) {
	for _, command := range []string{"check", "validate"} {
		var out, errb bytes.Buffer
		code := run([]string{command}, &out, &errb)
		if code != verifier.ExitUsage || !strings.Contains(errb.String(), "colligo check .") {
			t.Errorf("%s: exit %d stderr %q", command, code, errb.String())
		}
	}
}

func TestAFileURLIsAcceptedAsAPath(t *testing.T) {
	dir := t.TempDir()
	path := put(t, dir, "a.fastq", fastq("S", 5))
	code, out, errText := checkSource(t, nil, "file://"+path, "--color=never")
	if code != verifier.ExitOK || !strings.Contains(out, "1 file passes every check run") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errText)
	}
}

func TestSenselessCombinationsAreUsageErrorsThatNameBothFlags(t *testing.T) {
	dir := t.TempDir()
	sheet := put(t, dir, "s.csv", []byte("sample,fastq_1,fastq_2\nA,a.fastq,\n"))
	list := put(t, dir, "l.txt", []byte("a.fastq\n"))
	cases := map[string][]string{
		"samplesheet with files-from": {"--samplesheet", sheet, "--files-from", list},
		"files-from with expected":    {"--files-from", list, "--expected", "md5=00000000000000000000000000000000"},
		"files-from with recursive":   {"--files-from", list, "--recursive"},
		"a missing list":              {"--files-from", filepath.Join(dir, "nope.txt")},
		"a folder in the list":        {"--files-from", put(t, dir, "d.txt", []byte(dir+"\n"))},
	}
	for name, args := range cases {
		var out, errb bytes.Buffer
		code := runCheck(args, &out, &errb, false, nil)
		if code != verifier.ExitUsage || out.Len() != 0 || errb.Len() == 0 {
			t.Errorf("%s: exit %d stdout %q stderr %q", name, code, out.String(), errb.String())
		}
	}
}

func TestACommonMistakeIsNamedAndStillBlocks(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct {
		data []byte
		want []string
	}{
		"reads.sra":  {append([]byte("NCBI.sra"), make([]byte, 40)...), []string{"input_sra", "fasterq-dump", "SRA archive"}},
		"bundle.zip": {append([]byte("PK\x03\x04"), make([]byte, 40)...), []string{"input_archive", "ZIP archive", "Extract"}},
		"x.cram":     {append([]byte("CRAM\x03\x00"), make([]byte, 40)...), []string{"input_alignment", "CRAM", "does not check yet"}},
	}
	for name, c := range cases {
		path := put(t, dir, name, c.data)
		code, out, _ := checkSource(t, nil, path, "--color=never")
		if code != verifier.ExitIncomplete || strings.Contains(out, "✓ VERIFIED") {
			t.Errorf("%s: exit %d\n%s", name, code, out)
		}
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Errorf("%s: report lacks %q:\n%s", name, want, out)
			}
		}
	}
}

func TestASpreadsheetGivenAsASamplesheetIsAUsageErrorThatSaysToSaveAsCSV(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"samples.xlsx": append([]byte("PK\x03\x04"), make([]byte, 40)...),
		"samples.xls":  append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 40)...),
	} {
		code, out, errText := checkSource(t, nil, "--samplesheet", put(t, dir, name, data))
		if code != verifier.ExitUsage || out != "" || !strings.Contains(errText, "CSV") || !strings.Contains(errText, "spreadsheet") {
			t.Errorf("%s: exit %d stdout %q stderr %q", name, code, out, errText)
		}
	}
}

func TestAnUnknownFlagNamesTheClosestKnownOneAndDoesNotPrintAll(t *testing.T) {
	cases := map[string]string{"--sorce": "--source", "--recursve": "--recursive", "-files-form": "--files-from", "--samplesheat": "--samplesheet"}
	for typed, want := range cases {
		var out, errb bytes.Buffer
		code := run([]string{"check", typed, "x"}, &out, &errb)
		text := errb.String()
		if code != verifier.ExitUsage || !strings.Contains(text, "did you mean "+want+"?") {
			t.Errorf("%s: exit %d stderr %q", typed, code, text)
		}
		if strings.Count(text, "\n") > 2 {
			t.Errorf("%s: the whole flag list was printed:\n%s", typed, text)
		}
	}
	var out, errb bytes.Buffer
	if code := run([]string{"check", "--zzzzzzzzzz", "x"}, &out, &errb); code != verifier.ExitUsage || strings.Contains(errb.String(), "did you mean") {
		t.Errorf("gibberish got a suggestion: %q", errb.String())
	}
}

func TestHelpSaysWhatTheCommandDoesGivesExamplesAndTheExitCodes(t *testing.T) {
	for _, command := range []string{"check", "validate", "identify", "explain"} {
		var out, errb bytes.Buffer
		code := run([]string{command, "-h"}, &out, &errb)
		text := errb.String() + out.String()
		if code != verifier.ExitOK {
			t.Errorf("%s -h: exit %d", command, code)
		}
		for _, want := range []string{"usage: colligo " + command, "examples:", "colligo " + command, "exit codes:", "3  blocking", "flags:"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s -h lacks %q:\n%s", command, want, text)
			}
		}
	}
}

func TestIdentifyAndExplainSuggestTheFlagTheyMeant(t *testing.T) {
	for _, args := range [][]string{{"identify", "--jsno", "x"}, {"explain", "--colour", "count_mismatch"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != verifier.ExitUsage || !strings.Contains(errb.String(), "did you mean") {
			t.Errorf("%v: exit %d stderr %q", args, code, errb.String())
		}
	}
}
