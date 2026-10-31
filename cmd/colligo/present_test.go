package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/transport"
	"github.com/colligo-go/colligo/verifier"
)

var escapes = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

type refusingTransport struct{ calls int }

func (r *refusingTransport) Get(_ context.Context, _ secret.Endpoint, _ *transport.ByteRange) (*transport.Response, error) {
	r.calls++
	return nil, fmt.Errorf("the network must not be touched without --source")
}

func TestExitCodesDoNotDependOnHowTheReportIsPresented(t *testing.T) {
	want := map[string]int{
		"verified": 0, "source-verified": 0, "study": 3, "blocking": 3, "advisory": 0, "not-declared": 0, "unreadable": 2,
		"samplesheet-swapped": 3, "source-link": 0, "source-deduced": 0, "list-missing-line": 2, "sra-archive": 3, "identify-fasta": 0, "identify-gtf-declared": 0, "identify-gtf-conflict": 3, "identify-gtf-checksum": 0,
	}
	styles := [][]string{{}, {"--json"}, {"--quiet"}, {"--verbose"}, {"--color=always"}, {"--color=never"}, {"--json", "--color=always", "--verbose"}}
	for _, c := range goldenCases() {
		for _, style := range styles {
			dir := t.TempDir()
			if code, _ := c.prepare(t, dir)(style...); code != want[c.name] {
				t.Errorf("%s %v: exit %d, want %d", c.name, style, code, want[c.name])
			}
		}
	}
}

func TestJSONIsTheSameWhateverTheStylingFlags(t *testing.T) {
	for _, c := range goldenCases() {
		dir := t.TempDir()
		run := c.prepare(t, dir)
		_, base := run("--json")
		for _, extra := range [][]string{{"--color=always"}, {"--verbose"}, {"--quiet"}} {
			if _, got := run(append([]string{"--json"}, extra...)...); got != base {
				t.Errorf("%s: %v changed the JSON document", c.name, extra)
			}
		}
	}
}

func TestColourOutputIsThePlainOutputWithEscapes(t *testing.T) {
	for _, c := range goldenCases() {
		dir := t.TempDir()
		run := c.prepare(t, dir)
		_, plain := run("--color=never")
		_, colour := run("--color=always")
		if strings.Contains(plain, "\x1b") {
			t.Errorf("%s: --color=never wrote an escape", c.name)
		}
		if !strings.Contains(colour, "\x1b[") {
			t.Errorf("%s: --color=always wrote no colour", c.name)
		}
		strip := func(s string) string {
			return elapsedPattern.ReplaceAllString(escapes.ReplaceAllString(s, ""), "${1}<time>")
		}
		if strip(colour) != strip(plain) {
			t.Errorf("%s: colour changes the words, not only their colour", c.name)
		}
	}
}

func TestNothingIsColouredOutsideATerminalOrWithNoColor(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "s_R1.fastq", fastq("S", 5))
	t.Setenv("NO_COLOR", "")
	for _, env := range []string{"", "1"} {
		t.Setenv("NO_COLOR", env)
		_, out, _ := invoke("check", dir)
		if strings.Contains(out, "\x1b") {
			t.Errorf("NO_COLOR=%q: a buffer is not a terminal, yet the output has an escape:\n%q", env, out)
		}
	}
	t.Setenv("NO_COLOR", "1")
	if _, out, _ := invoke("check", "--color=always", dir); !strings.Contains(out, "\x1b[") {
		t.Error("--color=always must override NO_COLOR")
	}
	if _, out, _ := invoke("identify", "--color=always", put(t, dir, "x.fa", []byte(cliFasta))); !strings.Contains(out, "\x1b[") {
		t.Error("identify must take --color too")
	}
}

func TestQuietPrintsOneLineAndVerboseLists(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "s_R1.fastq", fastq("S", 5))
	put(t, dir, "notes.txt", []byte("notes"))
	_, quiet, _ := invoke("check", "--quiet", dir)
	if strings.Count(quiet, "\n") != 1 || !strings.HasPrefix(quiet, "✓ VERIFIED") {
		t.Errorf("--quiet:\n%q", quiet)
	}
	_, verbose, _ := invoke("check", "--verbose", dir)
	for _, want := range []string{"notes.txt: not a recognised data file", "✓ html_body"} {
		if !strings.Contains(verbose, want) {
			t.Errorf("--verbose lacks %q:\n%s", want, verbose)
		}
	}
}

func TestPresentationFlagUsageErrors(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "s.fastq", fastq("S", 5))
	for _, args := range [][]string{
		{"check", "--color=sometimes", dir},
		{"check", "--quiet", "--verbose", dir},
		{"identify", "--color=sometimes", dir},
		{"identify", "--quiet", "--verbose", dir},
		{"explain", "--color=sometimes", "count_mismatch"},
	} {
		if code, out, errText := invoke(args...); code != verifier.ExitUsage || out != "" || errText == "" {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out, errText)
		}
	}
}

func TestWithoutSourceAnEnaLookingFolderIsNotCheckedAndTheNetworkIsNeverTouched(t *testing.T) {
	spec := specOf(t, "DRR021374")
	dir := t.TempDir()
	writeDecompressed(t, dir, syntheticRun(spec))
	net := &refusingTransport{}
	var out, errb bytes.Buffer
	code := runCheck([]string{dir}, &out, &errb, false, net)
	if code != verifier.ExitOK || net.calls != 0 {
		t.Fatalf("exit %d, the network was called %d times\n%s%s", code, net.calls, out.String(), errb.String())
	}
	for _, want := range []string{"Not checked", "comparison with a source: not made", "add --source ena:DRR021374"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report lacks %q:\n%s", want, out.String())
		}
	}
	if !strings.HasPrefix(out.String(), "✓ VERIFIED  2 files pass every check run") {
		t.Errorf("the verdict must not claim a comparison that was not made:\n%s", out.String())
	}
}

func TestProgressNeverAppearsWhenOutputIsNotATerminal(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "big.fastq", fastq("BIG", 200_000))
	var out, errb bytes.Buffer
	if code := runCheck([]string{"--jobs", "1", dir}, &out, &errb, false, nil); code != verifier.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if errb.Len() != 0 || strings.Contains(out.String(), "reading") || strings.Contains(out.String(), "\r") {
		t.Errorf("progress leaked: stderr %q, stdout has bar %v", errb.String(), strings.Contains(out.String(), "\r"))
	}
	errb.Reset()
	identify := filepath.Join(dir, "x.fa")
	os.WriteFile(identify, []byte(cliFasta), 0o644)
	var iout bytes.Buffer
	runIdentifyWith(t, &iout, &errb, identify)
	if errb.Len() != 0 {
		t.Errorf("identify wrote progress to stderr: %q", errb.String())
	}
}

func runIdentifyWith(t *testing.T, out, errb *bytes.Buffer, path string) {
	t.Helper()
	runIdentify([]string{path}, out, errb, cliReferences)
}

func TestExplainPrintsTheFourPartsAndNeedsNoNetwork(t *testing.T) {
	code, out, errText := invoke("explain", "count_mismatch")
	if code != verifier.ExitOK || errText != "" {
		t.Fatalf("exit %d: %s", code, errText)
	}
	for _, want := range []string{"count_mismatch  blocking · M15", "What it means", "Why it happens", "What to do", "How to confirm"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

func TestExplainOfAnUnknownIdIsAUsageErrorThatListsSimilarIds(t *testing.T) {
	code, out, errText := invoke("explain", "count_mismtach")
	if code != verifier.ExitUsage || out != "" {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	if !strings.Contains(errText, "count_mismatch") || !strings.Contains(errText, "not a diagnosis id") {
		t.Errorf("stderr:\n%s", errText)
	}
	code, _, errText = invoke("explain", "zzzzzz")
	if code != verifier.ExitUsage || !strings.Contains(errText, "read_failed") {
		t.Errorf("with nothing similar, every id is listed: exit %d\n%s", code, errText)
	}
	if code, _, errText = invoke("explain"); code != verifier.ExitUsage || !strings.Contains(errText, "give one diagnosis id") {
		t.Errorf("no id: exit %d\n%s", code, errText)
	}
}

func TestEveryIdTheReportsSuggestCanBeExplained(t *testing.T) {
	for _, c := range goldenCases() {
		dir := t.TempDir()
		_, out := c.prepare(t, dir)("--color=never")
		for _, line := range strings.Split(out, "\n") {
			if id, found := strings.CutPrefix(strings.TrimSpace(line), "colligo explain "); found {
				if code, _, errText := invoke("explain", id); code != verifier.ExitOK {
					t.Errorf("%s: the report suggests `colligo explain %s`, which fails: %s", c.name, id, errText)
				}
			}
		}
	}
}

func TestExplainAppearsInTheUsage(t *testing.T) {
	if _, out, _ := invoke("--help"); !strings.Contains(out, "explain") {
		t.Errorf("usage:\n%s", out)
	}
}

func TestTheProgressBarExistsOnlyForATerminal(t *testing.T) {
	counter, finish := (&presentation{color: "always"}).startProgress(&bytes.Buffer{})
	finish()
	if counter != nil {
		t.Fatal("a progress counter was created for a stderr that is not a terminal")
	}
}
