package main

import (
	"flag"
	"fmt"
	log "github.com/sirupsen/logrus"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/pointer"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
	"os"
	"strconv"
)


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

//bz: tested
// godel2: dine3-chan-race, no-race-mut-bad, prod-cons-race
// ../go2/race_checker/GoBench/Kubernetes/88331/main.go
// ../go2/race_checker/GoBench/Grpc/3090/main.go
// ../go2/race_checker/GoBench/Istio/8967/main.go
//
//CURRENT:
// cmd/callgraph/testdata/src/pkg/pkg.go  --> extra calls
// ../go2/race_checker/pointe_analysis_test/main.go
// pointer/testdata/channels.go
func main() {
	flag.Bool("ptrAnalysis", false, "Prints pointer analysis results. ")
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
		fmt.Println("packages contain errors")
		return
	} else if len(initial) == 0 {
		fmt.Println("package list empty")
		return
	}

	// Print the names of the source files
	// for each package listed on the command line.
	for nP, pkg := range initial {
		fmt.Println(pkg.ID, pkg.GoFiles)
		fmt.Println("Done  -- " + strconv.Itoa(nP+1) + " packages loaded")
	}
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

	//create my log file
	var logName string
	logName = "log_" + mains[0].String()
	logfile, err := os.OpenFile(logName, os.O_WRONLY|os.O_CREATE, 0600) //os.O_APPEND|
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	// Configure pointer analysis to build call-graph
	ptaConfig := &pointer.Config{
		Mains:             mains, //bz: NOW assume only one main
		Reflection:        false,
		BuildCallGraph:    true,
		Log:               logfile,
		CallSiteSensitive: true,
		K:                 2,
		LimitScope:        true, //bz: only consider app methods now
	}

	//*** compute pta here
	result, err := pointer.Analyze(ptaConfig) // conduct pointer analysis
	if err != nil {
		log.Fatal(err)
	}
	defer logfile.Close()
	log.SetOutput(logfile)
	fmt.Println("Done  -- PTA/CG Build; Go check gologfile for detail. " + strconv.Itoa(len(result.CallGraph.Nodes)))
}