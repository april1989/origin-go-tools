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
	"strings"
	"time"
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
// godel2: mytest/dine3-chan-race.go, mytest/no-race-mut-bad.go, mytest/prod-cons-race.go
// ../go2/race_checker/GoBench/Kubernetes/88331/main.go
// ../go2/race_checker/GoBench/Grpc/3090/main.go
// ../go2/race_checker/GoBench/Istio/8967/main.go
//
//CURRENT:
// cmd/callgraph/testdata/src/pkg/pkg.go
// ../go2/race_checker/pointe_analysis_test/main.go  --> bz: cannot identify ParallelizeUntil() holds the closure, too much assignments... + no program counter ...
//bz: attention !!! freevar !!!
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
	logName = "gologfile"
	logfile, err := os.Create(logName) //bz: i do not want messed up log, create/overwrite one each time
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	// Configure pointer analysis to build call-graph
	ptaConfig := &pointer.Config{
		Mains:             mains, //bz: NOW assume only one main
		Reflection:        false,
		BuildCallGraph:    true,
		Log:               logfile,
		//kcfa
		CallSiteSensitive: true,
		//origin
		//Origin:            true,
		//shared config
		K:                 2,
		LimitScope:        true, //bz: only consider app methods now
		DEBUG:             true, //bz: rm all printed out info in console
	}

	//*** compute pta here
	start := time.Now() //performance
	result, err := pointer.Analyze(ptaConfig) // conduct pointer analysis
	t := time.Now()
	elapsed := t.Sub(start)
	if err != nil {
		log.Fatal(err)
	}
	defer logfile.Close()
	log.SetOutput(logfile)
	fmt.Println("\nDone  -- PTA/CG Build; Using " + elapsed.String() + ". \nGo check gologfile for detail. ")

	if ptaConfig.DEBUG {
		fmt.Println("\nWe are going to print out call graph. If not desired, turn off DEBUG.")
		cgns := result.CallGraph.Nodes
		fmt.Println("#CGNode: " + strconv.Itoa(len(cgns)))
		for _, cgn := range cgns {
			if !strings.Contains(cgn.Func.String(), "command-line-arguments.") { continue } //we only want the app call edges
			fmt.Println(cgn.String())
			outs := cgn.Out
			for _, out := range outs {
				fmt.Println( "  -> " + out.Callee.String())
			}
		}

		fmt.Println("\nWe are going to print out queries. If not desired, turn off DEBUG.")
		queries := result.Queries
		inQueries := result.IndirectQueries
		fmt.Println("#Queries: " + strconv.Itoa(len(queries)) + "\n#Indirect Queries: " + strconv.Itoa(len(inQueries)))
		fmt.Println("Queries Detail: ")
		for v, ps := range queries {
			for _, p := range ps {
				fmt.Println(p.String() + " (SSA:" + v.String() + "): {" + p.PointsTo().String() + "}")
			}
		}
		fmt.Println("\nIndirect Queries Detail: ")
		for v, ps := range inQueries {
			for _, p := range ps {
				fmt.Println(p.String() + " (SSA:" + v.String() + "): {" + p.PointsTo().String() + "}")
			}
		}
	}
}