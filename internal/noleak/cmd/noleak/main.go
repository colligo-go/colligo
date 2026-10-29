package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/colligo-go/colligo/internal/noleak"
)

func main() { singlechecker.Main(noleak.Analyzer) }
