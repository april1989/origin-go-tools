package compare

import (
	"fmt"
	"github.tamu.edu/April1989/go_tools/go/pointer"
	"github.tamu.edu/April1989/go_tools/go/ssa"
)

//compute the common paths in a set of mains from a pkg

var (
	size  = 3 //how many cands can include
	cands []*pointer.ResultWCtx
)

//bz: add to compute common parts
func AddCandidate(res *pointer.ResultWCtx) {
	if len(cands) == size {
		return
	}
	cands = append(cands, res)
}

//compute common parts among all candidates
//we do it like this:
//for a func with its cgnode a in a shared contour,
// 1. we find its corrsponding func with cgnode b in another pta (same func same ctx)
// 2. we check their callers and calllees
// 3. we check their pts of parameters and return values
// 4. we check all pts for their enclosing constraints
// 5. if the same to all the above -> they are the same
// 6. we check its callees next
func ComputeCommonParts() {
	fmt.Println("\n\nCompute Common parts ... ") //only shared contours

	for fn, _ := range cands[0].CallGraph.Fn2CGNode { //let it be the base
		callers := cands[0].CallGraph.GetNodesForFn(fn)
		for i, cand := range cands {
			_, ok := cand.CallGraph.Fn2CGNode[fn]  //should have the same hashcode
			if !ok {
				fmt.Println("No such func in cand ", i, " @", fn.String() )
				continue
			}
			_callers := cand.CallGraph.GetNodesForFn(fn)
			compareAcross(fn, callers, _callers)
		}
	}
}

//compare callers with _callers
func compareAcross(fn *ssa.Function, callers []*pointer.Node, _callers []*pointer.Node) {

	
}


