package sortslice_test

import (
	"testing"

	"github.com/o2lab/go-tools/go/analysis/analysistest"
	"github.com/o2lab/go-tools/go/analysis/passes/sortslice"
)

func Test(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, sortslice.Analyzer, "a")
}
