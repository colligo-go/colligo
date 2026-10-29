// Command noleak runs the internal/noleak check, via go vet -vettool or as
// a standalone CI binary.
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/colligo-go/colligo/internal/noleak"
)

func main() { singlechecker.Main(noleak.Analyzer) }
