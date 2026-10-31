package theme_test

import (
	"strings"
	"testing"

	"github.com/colligo-go/colligo/render/theme"
)

func TestChooseColoursOnlyATerminalWithoutNoColor(t *testing.T) {
	cases := []struct {
		mode       theme.Mode
		isTerminal bool
		noColor    bool
		colour     bool
	}{
		{theme.Auto, true, false, true},
		{theme.Auto, false, false, false},
		{theme.Auto, true, true, false},
		{theme.Never, true, false, false},
		{theme.Never, true, true, false},
		{theme.Always, false, false, true},
		{theme.Always, false, true, true},
	}
	for _, c := range cases {
		got := theme.Choose(c.mode, c.isTerminal, c.noColor).Apply(theme.Bad, "x") != "x"
		if got != c.colour {
			t.Errorf("mode %d terminal %v NO_COLOR %v: colour %v, want %v", c.mode, c.isTerminal, c.noColor, got, c.colour)
		}
	}
}

func TestParseMode(t *testing.T) {
	for word, want := range map[string]theme.Mode{"auto": theme.Auto, "always": theme.Always, "never": theme.Never} {
		if got, ok := theme.ParseMode(word); !ok || got != want {
			t.Errorf("%s: %v %v", word, got, ok)
		}
	}
	if _, ok := theme.ParseMode("sometimes"); ok {
		t.Error("an unknown mode was accepted")
	}
}

func TestBothThemesCarryTheSameSymbols(t *testing.T) {
	plain, colour := theme.NoColor(), theme.Color()
	if plain.Verified != colour.Verified || plain.Failed != colour.Failed || plain.Advisory != colour.Advisory || plain.NotChecked != colour.NotChecked {
		t.Fatal("the colour theme must not be the only place a symbol differs")
	}
	for _, s := range []string{plain.Verified, plain.Failed, plain.Advisory, plain.NotChecked} {
		if s == "" {
			t.Fatal("a symbol is empty")
		}
	}
}

func TestPlainThemeNeverWritesAnEscape(t *testing.T) {
	plain := theme.NoColor()
	for _, role := range []theme.Role{theme.Plain, theme.Good, theme.Bad, theme.BadStrong, theme.Warn, theme.Dim, theme.Strong} {
		if got := plain.Apply(role, "text"); got != "text" {
			t.Errorf("role %d: %q", role, got)
		}
	}
}

func TestColourThemeWrapsAndResets(t *testing.T) {
	got := theme.Color().Apply(theme.Good, "ok")
	if !strings.HasPrefix(got, "\x1b[") || !strings.HasSuffix(got, "\x1b[0m") || !strings.Contains(got, "ok") {
		t.Errorf("%q", got)
	}
	if theme.Color().Apply(theme.Good, "") != "" {
		t.Error("an empty string must stay empty")
	}
}
