package text

import (
	"fmt"
	"io"
	"strings"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/render/format"
	"github.com/colligo-go/colligo/render/theme"
)

// Explain writes the long explanation of one catalogue entry.
func Explain(w io.Writer, entry diagnoses.Entry, long diagnoses.Explanation, o Options) error {
	t := o.Theme
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s\n", t.Apply(theme.Strong, entry.ID), t.Apply(theme.Dim, fmt.Sprintf("  %s · %s", entry.Severity, entry.Mode)))
	for _, section := range []struct{ title, body string }{
		{"What it means", long.Means},
		{"Why it happens", long.Why},
		{"What to do", long.Do},
		{"How to confirm", long.Confirm},
	} {
		fmt.Fprintf(&b, "\n%s\n", t.Apply(theme.Strong, section.title))
		width := 0
		if o.Width > 0 {
			width = max(o.Width-2, 20)
		}
		for _, line := range format.Wrap(section.body, width) {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
