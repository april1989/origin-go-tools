// The shadow command runs the shadow analyzer.
package main

import (
	"github.com/o2lab/go-tools/go/analysis/passes/shadow"
	"github.com/o2lab/go-tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(shadow.Analyzer) }
