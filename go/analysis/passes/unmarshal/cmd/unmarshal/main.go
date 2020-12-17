// The unmarshal command runs the unmarshal analyzer.
package main

import (
	"github.com/o2lab/go-tools/go/analysis/passes/unmarshal"
	"github.com/o2lab/go-tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(unmarshal.Analyzer) }
