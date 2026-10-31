package drift

import (
	"strings"
	"testing"
)

const header = "run_accession\tfastq_md5\tread_count\tstatus\n"

const row = "RUN1\t0123456789abcdef0123456789abcdef;fedcba9876543210fedcba9876543210\t10\tpublic\n"

func TestShapeOfClassifiesCellsWithoutKeepingThem(t *testing.T) {
	shape := ShapeOf([]byte(header + row))
	want := map[string]string{
		"run_accession": "word",
		"fastq_md5":     "md5-list",
		"read_count":    "integer",
		"status":        "word",
	}
	for name, class := range want {
		if got := strings.Join(shape.Classes[name], ","); got != class {
			t.Errorf("%s: class %q, want %q", name, got, class)
		}
	}
	if !shape.Tabular || shape.Ragged {
		t.Errorf("tabular %t ragged %t", shape.Tabular, shape.Ragged)
	}
}

func TestShapeOfHeaderOnlyHasColumnsAndNoClasses(t *testing.T) {
	shape := ShapeOf([]byte(header))
	if !shape.Tabular || len(shape.Header) != 4 || len(shape.Classes["read_count"]) != 0 {
		t.Errorf("%+v", shape)
	}
}

func TestShapeOfHTMLIsNotTabular(t *testing.T) {
	if ShapeOf([]byte("<html><body>500</body></html>\n")).Tabular {
		t.Error("html reported as tabular")
	}
}

func TestDiffSameShapeIsEmpty(t *testing.T) {
	if drift, notes := Diff(ShapeOf([]byte(header+row)), ShapeOf([]byte(header+row))); len(drift)+len(notes) != 0 {
		t.Errorf("%v %v", drift, notes)
	}
}

func TestDiffNamesEveryKindOfChange(t *testing.T) {
	cases := []struct {
		name string
		live string
		want string
		note bool
	}{
		{"column disappeared", "run_accession\tfastq_md5\tstatus\nRUN1\t" + strings.Split(row, "\t")[1] + "\tpublic\n", "column disappeared: read_count", false},
		{"column appeared", "run_accession\tfastq_md5\tread_count\tstatus\textra\n" + strings.TrimSuffix(row, "\n") + "\tx\n", "column appeared: extra", true},
		{"column moved", "fastq_md5\trun_accession\tread_count\tstatus\n" + strings.Split(row, "\t")[1] + "\tRUN1\t10\tpublic\n", "column moved: run_accession from position 1 to 2", true},
		{"type changed", strings.Replace(header+row, "\t10\t", "\t1.5e3\t", 1), "column read_count: cell classes were [integer], are now [word]", false},
		{"list became single", "run_accession\tfastq_md5\tread_count\tstatus\nRUN1\t0123456789abcdef0123456789abcdef\t10\tpublic\n", "column fastq_md5: cell classes were [md5-list], are now [md5]", false},
		{"ragged", header + row + "RUN2\t10\n", "rows: ragged was false, is now true", false},
		{"not tabular", "<html>\n", "table: tabular was true, is now false", false},
	}
	baseline := ShapeOf([]byte(header + row))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			drift, notes := Diff(baseline, ShapeOf([]byte(tc.live)))
			got, other := drift, notes
			if tc.note {
				got, other = notes, drift
			}
			if !contains(got, tc.want) || contains(other, tc.want) {
				t.Errorf("drift %q, notes %q, want %q among the %s", drift, notes, tc.want, map[bool]string{true: "notes", false: "drift"}[tc.note])
			}
		})
	}
}

func TestDiffNeverCarriesAValue(t *testing.T) {
	live := strings.Replace(header+row, "\t10\t", "\tsecretvalue\t", 1)
	drift, notes := Diff(ShapeOf([]byte(header+row)), ShapeOf([]byte(live)))
	for _, line := range append(drift, notes...) {
		if strings.Contains(line, "secretvalue") || strings.Contains(line, "0123456789abcdef") {
			t.Errorf("a value reached the report: %s", line)
		}
	}
}

func contains(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}
