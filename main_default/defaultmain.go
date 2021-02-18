package main

import (
	"flag"
	"fmt"
	/////////bz: the following two sets of imports are equivalent
	//"golang.org/x/tools/go/packages"
	//"golang.org/x/tools/go/pointer"
	//"golang.org/x/tools/go/ssa"
	//"golang.org/x/tools/go/ssa/ssautil"
	"github.tamu.edu/April1989/go_tools/go_default/packages"
	"github.tamu.edu/April1989/go_tools/go_default/pointer"
	"github.tamu.edu/April1989/go_tools/go_default/ssa"
	"github.tamu.edu/April1989/go_tools/go_default/ssa/ssautil"
	"os"
	"strconv"
	"time"
)

var maxTime time.Duration
var minTime time.Duration
var doLog = false
var doPerformance = true

// mainPackages returns the main packages to analyze.
// Each resulting package is named "main" and has a main function.
func findMainPackages(pkgs []*ssa.Package) ([]*ssa.Package, error) {
	var mains []*ssa.Package
	for _, p := range pkgs {
		if p != nil && p.Pkg.Name() == "main" && p.Func("main") != nil {
			mains = append(mains, p)
		}
	}
	if len(mains) == 0 {
		return nil, fmt.Errorf("no main packages")
	}
	return mains, nil
}

func main() {
	flag.Parse()
	args := flag.Args()
	cfg := &packages.Config{
		Mode:  packages.LoadAllSyntax, // the level of information returned for each package
		Dir:   "",                     // directory in which to run the build system's query tool
		Tests: false,                  // setting Tests will include related test packages
	}
	fmt.Println("Loading input packages...")
	initial, err := packages.Load(cfg, args...)
	if err != nil {
		return
	}
	if packages.PrintErrors(initial) > 0 {
		errSize := packages.PrintErrors(initial) //bz: errPkg will be nil in initial
		if errSize > 0 {
			println("Excluded the ", strconv.Itoa(errSize), " packages contain errors, due to the above errors. ")
			println("Continue   -- ")
		}
	} else if len(initial) == 0 {
		fmt.Println("Package list empty")
		return
	}
	fmt.Println("Done  -- ", strconv.Itoa(len(initial)), " packages loaded")

	// Create and build SSA-form program representation.
	prog, pkgs := ssautil.AllPackages(initial, 0)

	fmt.Println("Building SSA code for entire program...")
	prog.Build()
	fmt.Println("Done  -- SSA code built")

	mains, err := findMainPackages(pkgs)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("#TOTAL MAIN: ", strconv.Itoa(len(mains)), "\n")

	maxTime = 0
	minTime = 10000000000000

	//baseline: foreach
	start := time.Now() //performance
	for i, main := range mains {
		fmt.Println(i, " ", main.String())
		doEachMain(i, main)
		fmt.Println("=============================================================================")
	}
	t := time.Now()
	elapsed := t.Sub(start)
	fmt.Println("\n\nBASELINE All Done  -- PTA/CG Build.\nTOTAL: ", elapsed.String(), ".")
	fmt.Println("Max: ", maxTime.String(), ".")
	fmt.Println("Min: ", minTime.String(), ".")
	fmt.Println("Avg: ", (float32(elapsed.Milliseconds()) / float32(len(mains)-1) / float32(1000)), "s.")
}

func doEachMain(i int, main *ssa.Package) {
	var logfile *os.File
	if doLog { //create my log file
		logfile, _ = os.Create("/Users/Bozhen/Documents/GO2/go2/go_tools/_logs/full_log_" + strconv.Itoa(i))
	} else {
		logfile = nil
	}

	var mains []*ssa.Package
	mains = append(mains, main)
	// Configure pointer analysis to build call-graph
	ptaConfig := &pointer.Config{
		Mains:          mains, //one main per time
		Reflection:     false,
		BuildCallGraph: true,
		Log:            logfile,
		DoPerformance:  doPerformance, //bz: I add to output performance for comparison
	}

	//*** compute pta here
	start := time.Now()                       //performance
	result, err := pointer.Analyze(ptaConfig) // conduct pointer analysis
	t := time.Now()
	elapsed := t.Sub(start)
	if err != nil {
		fmt.Println(err)
	}
	defer logfile.Close()
	fmt.Println("\nDone  -- PTA/CG Build; Using ", elapsed.String(), ". \nGo check gologfile for detail. ")

	if maxTime < elapsed {
		maxTime = elapsed
	}
	if minTime > elapsed {
		minTime = elapsed
	}

	if len(result.Warnings) > 0 {
		fmt.Println("Warning: ", len(result.Warnings)) //bz: just do not report not used var on result
	}
}
