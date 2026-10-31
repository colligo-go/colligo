package progress_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/render/progress"
	"github.com/colligo-go/colligo/render/theme"
)

func TestAFastReadDrawsNothing(t *testing.T) {
	var out bytes.Buffer
	bar := progress.New(&out, theme.NoColor())
	bar.Update(10, 100)
	bar.Update(100, 100)
	bar.Finish()
	if out.Len() != 0 {
		t.Fatalf("a read that ends within the interval must draw nothing: %q", out.String())
	}
}

func TestSlowReadDrawsTheBarAndFinishErasesIt(t *testing.T) {
	var out bytes.Buffer
	bar := progress.New(&out, theme.NoColor())
	progress.AdvanceClock(bar, 200)
	bar.Update(50, 100)
	got := out.String()
	for _, want := range []string{"reading", "50%", "[############------------]", "50 B of 100 B"} {
		if !strings.Contains(got, want) {
			t.Errorf("bar lacks %q: %q", want, got)
		}
	}
	bar.Finish()
	if !strings.HasSuffix(out.String(), "\r\x1b[2K") {
		t.Errorf("Finish must erase the line: %q", out.String())
	}
	before := out.Len()
	bar.Finish()
	if out.Len() != before {
		t.Error("Finish must write nothing when nothing is drawn")
	}
}

func TestUpdateIsThrottled(t *testing.T) {
	var out bytes.Buffer
	bar := progress.New(&out, theme.NoColor())
	progress.AdvanceClock(bar, 200)
	bar.Update(10, 100)
	size := out.Len()
	bar.Update(20, 100)
	bar.Update(30, 100)
	if out.Len() != size {
		t.Error("updates inside the interval must not redraw")
	}
}

func TestUnknownTotalDrawsNothing(t *testing.T) {
	var out bytes.Buffer
	bar := progress.New(&out, theme.NoColor())
	progress.AdvanceClock(bar, 200)
	bar.Update(10, 0)
	if out.Len() != 0 {
		t.Errorf("%q", out.String())
	}
}
