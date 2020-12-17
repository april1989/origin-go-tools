// The findcall command runs the findcall analyzer.
package main

import (
	"github.com/o2lab/go-tools/go/analysis/passes/findcall"
	"github.com/o2lab/go-tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(findcall.Analyzer) }
