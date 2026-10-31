package text_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/render/text"
	"github.com/colligo-go/colligo/render/theme"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/verifier"
)

func options(code int) text.Options {
	return text.Options{Theme: theme.NoColor(), ExitCode: code}
}

func render(t *testing.T, r verifier.Report, o text.Options) string {
	t.Helper()
	var out strings.Builder
	if err := text.Check(&out, r, o); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func fastq(reads int) []byte {
	var b strings.Builder
	for i := 0; i < reads; i++ {
		b.WriteString("@r\nACGTACGTAC\n+\nFFFFFFFFFF\n")
	}
	return []byte(b.String())
}

func checked(t *testing.T, files map[string][]byte) verifier.Report {
	t.Helper()
	dir := t.TempDir()
	var names []secret.Name
	for _, base := range []string{"a.fastq", "b.fastq", "c.fastq", "ok.fastq"} {
		data, present := files[base]
		if !present {
			continue
		}
		path := filepath.Join(dir, base)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, secret.NewName(path))
	}
	report, err := verifier.CheckAll(t.Context(), names, verifier.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func failing(id string, severity model.Severity) model.Assertion {
	return model.Assertion{ID: id, Mode: "M1", Severity: severity, Observation: "observed " + id, Cause: "cause of " + id, Action: "action for " + id}
}

func TestVerdictIsTheFirstLineWithSymbolWordAndExitCode(t *testing.T) {
	healthy := checked(t, map[string][]byte{"ok.fastq": fastq(5)})
	broken := checked(t, map[string][]byte{"a.fastq": fastq(5)[:40]})
	cases := []struct {
		name   string
		report verifier.Report
		code   int
		want   string
	}{
		{"verified", healthy, 0, "✓ VERIFIED  1 file passes every check run  exit 0"},
		{"failed", broken, 3, "✗ FAILED  1 of 1 file failed a check  exit 3"},
		{"nothing", verifier.Report{}, 0, "! NOTHING CHECKED  no data file was found  exit 0"},
		{"quick", verifier.Report{Files: healthy.Files, Quick: true}, 5, "! QUICK  1 file read in part: a quick check is never verified  exit 5"},
	}
	for _, c := range cases {
		out := render(t, c.report, options(c.code))
		if first := strings.SplitN(out, "\n", 2)[0]; first != c.want {
			t.Errorf("%s: first line %q, want %q", c.name, first, c.want)
		}
	}
}

func TestPermissiveNeverHidesTheFailure(t *testing.T) {
	broken := checked(t, map[string][]byte{"a.fastq": fastq(5)[:40]})
	o := options(0)
	o.Permissive = true
	first := strings.SplitN(render(t, broken, o), "\n", 2)[0]
	if !strings.HasPrefix(first, "✗ FAILED") || !strings.HasSuffix(first, "exit 0 (--permissive)") {
		t.Errorf("%q", first)
	}
}

func TestAProblemIsStatedOnceHoweverManyFilesShareIt(t *testing.T) {
	broken := checked(t, map[string][]byte{"a.fastq": fastq(5)[:40], "b.fastq": fastq(5)[:40], "c.fastq": fastq(5)[:40]})
	out := render(t, broken, options(3))
	for _, once := range []string{"structure_invalid · blocking", "Likely", "Do  "} {
		if got := strings.Count(out, once); got != 1 {
			t.Errorf("%q appears %d times, want once:\n%s", once, got, out)
		}
	}
	if !strings.Contains(out, "in a.fastq, b.fastq, c.fastq") {
		t.Errorf("the files that share the problem are not named:\n%s", out)
	}
}

func TestQuietPrintsOnlyTheVerdict(t *testing.T) {
	broken := checked(t, map[string][]byte{"a.fastq": fastq(5)[:40]})
	o := options(3)
	o.Quiet = true
	out := render(t, broken, o)
	if strings.Count(out, "\n") != 1 || !strings.HasPrefix(out, "✗ FAILED") {
		t.Errorf("%q", out)
	}
}

func TestColligosOwnOutputsAreHiddenUnlessVerbose(t *testing.T) {
	report := checked(t, map[string][]byte{"ok.fastq": fastq(5)})
	report.Skipped = []string{"dir/notes.txt", "dir/ok.fastq.colligo-cert.json", "dir/colligo.sha256", "dir/colligo.b3"}
	plain := render(t, report, options(0))
	for _, own := range []string{"colligo-cert", "colligo.sha256", "colligo.b3"} {
		if strings.Contains(plain, own) {
			t.Errorf("default output names %s:\n%s", own, plain)
		}
	}
	if !strings.Contains(plain, "1 other file skipped") {
		t.Errorf("the one real skipped file must still be counted:\n%s", plain)
	}
	o := options(0)
	o.Verbose = true
	verbose := render(t, report, o)
	for _, want := range []string{"dir/notes.txt", "dir/ok.fastq.colligo-cert.json", "dir/colligo.sha256", "dir/colligo.b3", "✓ html_body"} {
		if !strings.Contains(verbose, want) {
			t.Errorf("verbose output lacks %q:\n%s", want, verbose)
		}
	}
}

func TestProseWrapsToTheWidthAndTablesDoNot(t *testing.T) {
	broken := checked(t, map[string][]byte{"a.fastq": fastq(5)[:40]})
	o := options(3)
	o.Width = 60
	out := render(t, broken, o)
	for _, line := range strings.Split(out, "\n") {
		if n := len([]rune(line)); n > 60 {
			t.Errorf("%d columns: %q", n, line)
		}
	}
	if !strings.Contains(out, "Likely") {
		t.Fatalf("no issue in the report:\n%s", out)
	}
}

func TestEveryFailedIdGetsItsExplainCommandOnce(t *testing.T) {
	broken := checked(t, map[string][]byte{"a.fastq": fastq(5)[:40], "b.fastq": fastq(5)[:40]})
	out := render(t, broken, options(3))
	if got := strings.Count(out, "colligo explain structure_invalid"); got != 1 {
		t.Errorf("explain line appears %d times:\n%s", got, out)
	}
	healthy := checked(t, map[string][]byte{"ok.fastq": fastq(5)})
	if out := render(t, healthy, options(0)); strings.Contains(out, "colligo explain") {
		t.Errorf("a clean report must not suggest explain:\n%s", out)
	}
}

func TestColourIsOnlyEscapesAroundTheSameText(t *testing.T) {
	broken := checked(t, map[string][]byte{"a.fastq": fastq(5)[:40]})
	plain := render(t, broken, options(3))
	o := options(3)
	o.Theme = theme.Color()
	colour := render(t, broken, o)
	if !strings.Contains(colour, "\x1b[") {
		t.Fatal("the colour theme wrote no colour")
	}
	if got := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(colour, ""); got != plain {
		t.Errorf("colour changes the words, not only their colour:\n--- plain\n%s\n--- colour\n%s", plain, got)
	}
	if strings.Contains(plain, "\x1b") {
		t.Error("the plain theme wrote an escape")
	}
}

func int64p(n int64) *int64 { return &n }

func studyReport() verifier.Report {
	name := func(s string) secret.Name { return secret.NewName(s) }
	local := name("/data/SRR1_1.fastq.gz")
	return verifier.Report{Source: &verifier.SourceReport{ID: "ena", Accession: "PRJ1", Runs: []*verifier.RunReport{
		{Accession: "SRR1", Layout: "PAIRED", Status: "public", Declared: model.RunCounts{BaseCount: int64p(3454158)},
			Measured: &verifier.RunTotals{Reads: 10, Bases: 3454158},
			Files:    []verifier.RunFile{{Name: name("SRR1_1.fastq.gz"), Local: &local, LinkedBy: verifier.LinkedByName}},
			Assertions: []model.Assertion{
				{ID: diagnoses.CountMismatch, Severity: model.Blocking, Passed: true}}},
		{Accession: "SRR2", Layout: "SINGLE", Status: "public", Declared: model.RunCounts{BaseCount: int64p(3454158)},
			Measured:   &verifier.RunTotals{Reads: 10, Bases: 1747050},
			Assertions: []model.Assertion{failing(diagnoses.CountMismatch, model.Blocking)}},
		{Accession: "SRR3", Layout: "SINGLE", Status: "public"},
	}}}
}

func TestSummaryTableHasOneRowPerRunWithTheDifferenceInPercent(t *testing.T) {
	out := render(t, studyReport(), options(3))
	if !strings.HasPrefix(out, "✗ FAILED  1 of 3 runs of PRJ1 do not match ENA: do not use their files  exit 3\n") {
		t.Errorf("verdict:\n%s", out)
	}
	for _, want := range []string{"SRR1  PAIRED", "1/1", "0/0", "3,454,158", "-49.4%", "✗ fails", "✓ matches", "not declared"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
}

func TestOnlyTheRunsThatFailedGetDetails(t *testing.T) {
	out := render(t, studyReport(), options(3))
	if strings.Count(out, "Run SRR2") != 1 || strings.Contains(out, "Run SRR1") || strings.Contains(out, "Run SRR3") {
		t.Errorf("details must follow only the failed run:\n%s", out)
	}
	if !strings.Contains(out, "3,454,158 declared by ENA, 1,747,050 observed (-49.4%)") {
		t.Errorf("the numbers of the failed run are missing:\n%s", out)
	}
}

func TestFailedRunsComeBeforeRunsWithANote(t *testing.T) {
	r := studyReport()
	r.Source.Runs[0].Assertions = []model.Assertion{failing(diagnoses.LayoutMismatch, model.Advisory)}
	out := render(t, r, options(3))
	if strings.Index(out, "Run SRR2") > strings.Index(out, "Run SRR1") || !strings.Contains(out, "Run SRR1") {
		t.Errorf("failures must come first:\n%s", out)
	}
}

func TestWithoutASourceEnaNamesGetTheSuggestionAndOtherNamesDoNot(t *testing.T) {
	file := func(base string) *verifier.FileReport {
		return &verifier.FileReport{Name: secret.NewName("/data/" + base), Status: verifier.StatusVerified, Assertions: []model.Assertion{}}
	}
	ena := verifier.Report{Files: []*verifier.FileReport{file("DRR021374_1.fastq.gz"), file("DRR021374_2.fastq.gz")}}
	out := render(t, ena, options(0))
	for _, want := range []string{"Not checked", "comparison with a source: not made", "add --source ena:DRR021374"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	other := verifier.Report{Files: []*verifier.FileReport{file("sample_R1.fastq.gz")}}
	if out := render(t, other, options(0)); strings.Contains(out, "--source") || !strings.Contains(out, "comparison with a source: not made") {
		t.Errorf("a name that is not an accession must not get a suggestion:\n%s", out)
	}
	several := verifier.Report{Files: []*verifier.FileReport{file("ERR1_1.fastq"), file("SRR2_1.fastq")}}
	if out := render(t, several, options(0)); !strings.Contains(out, "look like 2 ENA runs (ERR1, SRR2)") {
		t.Errorf("several runs:\n%s", out)
	}
	withSource := studyReport()
	if out := render(t, withSource, options(3)); strings.Contains(out, "comparison with a source: not made") {
		t.Errorf("a report that has a source must not say it was not compared:\n%s", out)
	}
}

func TestFileLinkedByContentShowsTheNameTheSourceUses(t *testing.T) {
	local := secret.NewName("/data/sample1_R1.fastq.gz")
	file := &verifier.FileReport{Name: local, Status: verifier.StatusVerified, Assertions: []model.Assertion{}}
	r := verifier.Report{Files: []*verifier.FileReport{file}, Source: &verifier.SourceReport{ID: "ena", Accession: "SRR1", Runs: []*verifier.RunReport{{
		Accession: "SRR1", Layout: "SINGLE", Status: "public",
		Files:      []verifier.RunFile{{Name: secret.NewName("SRR1_1.fastq.gz"), Local: &local, LinkedBy: verifier.LinkedByMD5}},
		Assertions: []model.Assertion{failing(diagnoses.CountMismatch, model.Blocking)},
	}}}}
	if out := render(t, r, options(3)); !strings.Contains(out, "sample1_R1.fastq.gz  (for SRR1_1.fastq.gz)") {
		t.Errorf("%s", out)
	}
}

func TestNothingIsDrawnFromARedactedNameBeyondItsDisplayForm(t *testing.T) {
	remote := &verifier.FileReport{Name: secret.NewName("https://host.example/a.fq.gz?token=SECRET"), Status: verifier.StatusInvalid,
		Assertions: []model.Assertion{failing(diagnoses.SheetRemotePath, model.Blocking)}}
	out := render(t, verifier.Report{Files: []*verifier.FileReport{remote}, FromSamplesheet: true}, options(3))
	if !strings.Contains(out, "samplesheet: ") {
		t.Errorf("a samplesheet report must say so:\n%s", out)
	}
}
