package pointer

import (
	"fmt"
	"github.tamu.edu/April1989/go_tools/go/ssa"
	"strconv"
	"strings"
)

//bz: compute the common paths in a set of mains from a pkg

var (
	size  = 60 //how many cands can include
	cands []*ResultWCtx
	base  *ResultWCtx
	//result
	sames = make(map[*ssa.Function][]string)
	diffs = make(map[*ssa.Function][]string)
)

//bz: add to compute common parts
func AddCandidate(res *ResultWCtx) {
	if len(cands) == size {
		return
	}
	cands = append(cands, res)
}

//bz: we select the one with the most fns as the base
func selectBase() {
	max := len(cands[0].CallGraph.Fn2CGNode)
	for _, cand := range cands {
		l := len(cand.CallGraph.Fn2CGNode)
		if l > max {
			max = l
			base = cand
		}
	}
	fmt.Println("Base: ", base.main.fn.Pkg.String(), "\n")
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
	fmt.Println("\n\nCompute Common parts ... \n") //only shared contours

	selectBase() //select base

	for fn := range base.CallGraph.Fn2CGNode {
		callers := base.CallGraph.GetNodesForFn(fn)

		//select shared contour cgnodes
		var baseNodes []*Node
		for _, caller := range callers {
			if caller.GetCGNode().IsSharedContour() {
				baseNodes = append(baseNodes, caller)
			}
		}
		if len(baseNodes) == 0 {
			//fmt.Println("No shared contour in bases @", fn.String() )
			continue
		}
		if len(baseNodes) > 1 {
			fmt.Println("** Multiple shared contour bases @"+fn.String())
			continue
		}

		for i, cand := range cands {
			if cand == base {
				continue
			}
			_, ok := cand.CallGraph.Fn2CGNode[fn] //should have the same hashcode
			if !ok {
				fmt.Println("No such func in cand ", i, " @", fn.String())
				continue
			}
			_callers := cand.CallGraph.GetNodesForFn(fn)
			compareAcross(fn, baseNodes[0], i, _callers)
		}
	}

	//print out result
	fmt.Println("SAME: ", len(sames), "/", len(base.CallGraph.Fn2CGNode), ".")
	fmt.Println("DIFF: ", len(diffs), "/", len(base.CallGraph.Fn2CGNode), ".")

	fmt.Println("\nSame Detail: ")
	for fn, details := range sames {
		fmt.Println(fn)
		for _, detail := range details {
			fmt.Println(detail)
		}
	}

	fmt.Println("\n\nDiff Detail: ")
	for fn, details := range diffs {
		fmt.Println(fn)
		for _, detail := range details {
			fmt.Println(detail)
		}
	}
}

func updateSame(fn *ssa.Function, s string) {
	same := sames[fn]
	same = append(same, s)
	sames[fn] = same
}

func updateDiff(fn *ssa.Function, s string) {
	diff := diffs[fn]
	diff = append(diff, s)
	diffs[fn] = diff
}

//compare callers with _callers; only for shared contour
func compareAcross(fn *ssa.Function, base *Node, ithCand int, _callers []*Node) {
	//select shared contour cgnodes
	var comparees []*Node
	for _, _caller := range _callers {
		if _caller.GetCGNode().IsSharedContour() {
			comparees = append(comparees, _caller)
		}
	}
	if len(comparees) == 0 {
		//fmt.Println("No shared contour in comparees @", fn.String() )
		return
	}
	if len(comparees) > 1 {
		fmt.Println("** Multiple shared contour comparees @"+fn.String())
		return
	}

	//fmt.Println("Comparing ... ", fn.String())

	//compare started
	comparee := comparees[0]

	//compare pts
	base_cgn := base.GetCGNode()
	comparee_cgn := comparee.GetCGNode()
	same := compareCGNode(base_cgn, ithCand, comparee_cgn)

	if !same {
		updateDiff(fn, "- "+strconv.Itoa(ithCand)+"'s cand -> DIFF PTS")
		return
	}

	//compare In and Out
	base_ins := base.In
	comparee_ins := comparee.In
	if len(base_ins) != len(comparee_ins) {
		updateDiff(fn, "- "+strconv.Itoa(ithCand)+"'s cand -> DIFF INS (len)")
		return
	}

	base_outs := base.Out
	comparee_outs := comparee.Out

	if len(base_outs) != len(comparee_outs) {
		updateDiff(fn, "- "+strconv.Itoa(ithCand)+"'s cand -> DIFF OUTS (len)")
		return
	}

	for _, baseIn := range base_ins {
		baseSite := baseIn.Site
		baseCaller := baseIn.Caller.GetCGNode()
		for _, compareeIn := range comparee_ins {
			compareeSite := compareeIn.Site
			compareeCaller := compareeIn.Caller.GetCGNode()
			if ((baseSite == nil && compareeSite == nil) || (baseSite.String() == compareeSite.String())) &&
				baseCaller.fn.String() == compareeCaller.fn.String() &&
				len(baseCaller.callersite) == len(compareeCaller.callersite) {
				//cannot find a better way to do equal check
				for i := 0; i < len(baseCaller.callersite); i++ { //caller context cannot be determined
					basectx := baseCaller.callersite[i]
					compareectx := compareeCaller.callersite[i]
					if !basectx.relaxEqual(compareectx) {
						updateDiff(fn, "- "+strconv.Itoa(ithCand)+"'s cand -> DIFF INS (detail)")
						return
					}
				}
				break // find the match, go check next
			}
		}
	}

	for _, baseOut := range base_outs {
		baseSite := baseOut.Site
		baseCallee := baseOut.Callee.GetCGNode()
		for _, compareeOut := range comparee_outs {
			compareeSite := compareeOut.Site
			compareeCallee := compareeOut.Callee.GetCGNode()
			if ((baseSite == nil && compareeSite == nil) || (baseSite.String() == compareeSite.String())) &&
				baseCallee.fn.String() == compareeCallee.fn.String() &&
				len(baseCallee.callersite) == len(compareeCallee.callersite) {
				//cannot find a better way to do equal check
				//caller ctx is shared contour, callee must be shared contour
				break // find the match, go check next
			} else {
				updateDiff(fn, "- "+strconv.Itoa(ithCand)+"'s cand -> DIFF OUTS (detail)")
				return
			}
		}
	}

	updateSame(fn, "- "+strconv.Itoa(ithCand)+"'s cand -> SAME")
}

//bz: whether n and o have the same sites (same target),
//callersite (shared contour -> skip compare), actualCallerSite (skip now),
//and localval, localobj -> have the same pts
func compareCGNode(n *cgnode, ith int, o *cgnode) bool {
	na := base.a
	oa := cands[ith].a

	////compare sites -> should be the same if IRs are the same ==> this should be duplicate with the check of *Node.Out
	//for i := 1; i < len(n.sites); i++ {
	//	//check target, should be enough
	//	ntid := n.sites[i].targets
	//	otid := o.sites[i].targets
	//	same := samePTS(ntid, otid, na, oa)
	//	if !same {
	//		return false
	//	}
	//}

	if len(n.localval) != len(o.localval) || len(n.localobj) != len(o.localobj) {
		return false
	}

	//compare localval
	for val, nid := range n.localval {
		oid := o.localval[val] //should have the same hashcode; and should also have the same order of storing val
		same := samePTS(nid, oid, na, oa)
		if !same {
			return false
		}
	}

	//compare localobj
	for val, nid := range n.localobj {
		oid := o.localobj[val] //should have the same hashcode
		same := samePTS(nid, oid, na, oa)
		if !same {
			return false
		}
	}

	return true
}

func samePTS(nid nodeid, oid nodeid, na *analysis, oa *analysis) bool {
	np := &PointsToSet{na, &na.nodes[nid].solve.pts}
	op := &PointsToSet{oa, &oa.nodes[oid].solve.pts}
	if np.pts.Len() == 0 && op.pts.Len() == 0 {
		return true //skip this empty check
	}
	if np.pts.Len() != op.pts.Len() {
		return false
	}

	n_pts := pts2Strings(np)
	o_pts := pts2Strings(op)
	for _, nobj := range n_pts {
		for _, oobj := range o_pts {
			if nobj == oobj {
				continue
			} else {
				return false
			}
		}
	}
	return true
}

func pts2Strings(p *PointsToSet) []string {
	var s_pts []string
	_pts := p.String()
	_pts = _pts[1 : len(_pts)-1]
	objs := strings.Split(_pts, ",")
	for _, obj := range objs {
		if obj == "" {
			continue
		}
		s_pts = append(s_pts, obj)
	}
	return s_pts
}
