package format_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/colligo-go/colligo/render/format"
)

func TestNumberHasThousandsSeparators(t *testing.T) {
	cases := map[int64]string{0: "0", 7: "7", 999: "999", 1000: "1,000", 3454158: "3,454,158", -1234567: "-1,234,567", 1000000000000: "1,000,000,000,000"}
	for n, want := range cases {
		if got := format.Number(n); got != want {
			t.Errorf("Number(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestSizeUsesHumanUnits(t *testing.T) {
	cases := map[int64]string{0: "0 B", 999: "999 B", 1000: "1.0 kB", 1500: "1.5 kB", 4_100_000: "4.1 MB", 2_000_000_000: "2.0 GB", 3_000_000_000_000: "3.0 TB"}
	for n, want := range cases {
		if got := format.Size(n); got != want {
			t.Errorf("Size(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestDurationReadsAtTheScaleOfTheWait(t *testing.T) {
	cases := map[time.Duration]string{
		40 * time.Millisecond: "40 ms", 1400 * time.Millisecond: "1.4 s",
		72 * time.Second: "1 m 12 s", 3725 * time.Second: "1 h 02 m",
	}
	for d, want := range cases {
		if got := format.Duration(d); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestDifferenceNeverShowsASmallGapAsZero(t *testing.T) {
	cases := []struct {
		observed, declared int64
		want               string
	}{
		{100, 100, "0%"},
		{1747050, 3454158, "-49.4%"},
		{102, 100, "+2.0%"},
		{3454157, 3454158, "-<0.1%"},
		{3454159, 3454158, "+<0.1%"},
		{5, 0, "n/a"},
	}
	for _, c := range cases {
		if got := format.Difference(c.observed, c.declared); got != c.want {
			t.Errorf("Difference(%d, %d) = %q, want %q", c.observed, c.declared, got, c.want)
		}
	}
}

func TestPerReadIsAMeasuredRatio(t *testing.T) {
	if got := format.PerRead(1733283, 13767); got != "125.9" {
		t.Errorf("got %q", got)
	}
	if got := format.PerRead(10, 0); got != "-" {
		t.Errorf("no reads must not divide: %q", got)
	}
}

func TestWrapBreaksOnlyBetweenWords(t *testing.T) {
	text := "one two three four five six seven eight nine ten"
	for _, width := range []int{10, 15, 22} {
		for _, line := range format.Wrap(text, width) {
			if format.Width(line) > width {
				t.Errorf("width %d: %q is %d wide", width, line, format.Width(line))
			}
		}
		if got := strings.Join(format.Wrap(text, width), " "); got != text {
			t.Errorf("width %d lost words: %q", width, got)
		}
	}
	if got := format.Wrap(text, 0); len(got) != 1 || got[0] != text {
		t.Errorf("a zero width must not wrap: %v", got)
	}
	if got := format.Wrap("unbreakablewordlongerthanthewidth", 5); len(got) != 1 {
		t.Errorf("a long word stays whole: %v", got)
	}
}

func TestTableAlignsAndNeverEndsInSpaces(t *testing.T) {
	rows := [][]format.Cell{
		{format.Text("name"), format.Num("reads"), format.Text("result")},
		{format.Text("a.fastq"), format.Num("13,767"), format.Text("ok")},
		{format.Text("bb.fastq"), format.Num("5"), format.Text("failed")},
	}
	lines := format.Table(rows, "  ", 2)
	want := []string{
		"  name       reads  result",
		"  a.fastq   13,767  ok",
		"  bb.fastq       5  failed",
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestTableStyleDoesNotChangeTheAlignment(t *testing.T) {
	bold := func(s string) string { return "\x1b[1m" + s + "\x1b[0m" }
	rows := [][]format.Cell{
		{format.Text("a").Styled(bold), format.Text("x")},
		{format.Text("bbb"), format.Text("y")},
	}
	lines := format.Table(rows, "", 1)
	if lines[0] != "\x1b[1ma\x1b[0m   x" || lines[1] != "bbb y" {
		t.Errorf("%q", lines)
	}
}

func TestABufferIsNotATerminal(t *testing.T) {
	if is, width := format.Terminal(&bytes.Buffer{}); is || width != 0 {
		t.Errorf("a buffer reported as a terminal: %v %d", is, width)
	}
}

func TestPlural(t *testing.T) {
	if format.Plural(1, "file", "files") != "1 file" || format.Plural(0, "file", "files") != "0 files" || format.Plural(1500, "file", "files") != "1,500 files" {
		t.Error("plural forms")
	}
}
