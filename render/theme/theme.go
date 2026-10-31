// Package theme holds the symbols and colours of the text report in one
// value, so another look is another Theme and not another layout.
package theme

type Role int

const (
	Plain Role = iota
	Good
	Bad
	BadStrong
	Warn
	Dim
	Strong
)

type Theme struct {
	Verified   string
	Failed     string
	Advisory   string
	NotChecked string
	codes      map[Role]string
}

const reset = "\x1b[0m"

func NoColor() Theme {
	return Theme{Verified: "✓", Failed: "✗", Advisory: "!", NotChecked: "–"}
}

func Color() Theme {
	t := NoColor()
	t.codes = map[Role]string{
		Good:      "\x1b[32m",
		Bad:       "\x1b[31m",
		BadStrong: "\x1b[1;31m",
		Warn:      "\x1b[33m",
		Dim:       "\x1b[2m",
		Strong:    "\x1b[1m",
	}
	return t
}

func (t Theme) Apply(role Role, s string) string {
	code, ok := t.codes[role]
	if !ok || s == "" {
		return s
	}
	return code + s + reset
}

func (t Theme) Style(role Role) func(string) string {
	return func(s string) string { return t.Apply(role, s) }
}

type Mode int

const (
	Auto Mode = iota
	Always
	Never
)

func ParseMode(s string) (Mode, bool) {
	switch s {
	case "auto":
		return Auto, true
	case "always":
		return Always, true
	case "never":
		return Never, true
	}
	return Auto, false
}

// Choose picks the theme from the facts the CLI observed. Colour is on for a
// terminal unless NO_COLOR is set; --color=always and --color=never override
// both.
func Choose(mode Mode, isTerminal, noColor bool) Theme {
	switch {
	case mode == Always, mode == Auto && isTerminal && !noColor:
		return Color()
	}
	return NoColor()
}
