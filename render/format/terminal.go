package format

import (
	"io"
	"os"

	"golang.org/x/term"
)

// Terminal reports whether w is an interactive terminal and, if so, its
// width in columns. A pipe, a file or an in-memory buffer is not one.
func Terminal(w io.Writer) (isTerminal bool, width int) {
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return false, 0
	}
	columns, _, err := term.GetSize(int(f.Fd()))
	if err != nil || columns <= 0 {
		return true, 0
	}
	return true, columns
}
