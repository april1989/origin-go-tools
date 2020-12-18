// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pointer

import (
	"bytes"
	"fmt"
	"go/token"
	"io"
	"strconv"
	"strings"

	"github.tamu.edu/April1989/go_tools/container/intsets"
	"github.tamu.edu/April1989/go_tools/go/callgraph"
	"github.tamu.edu/April1989/go_tools/go/ssa"
	"github.tamu.edu/April1989/go_tools/go/types/typeutil"
)

// A Config formulates a pointer analysis problem for Analyze. It is
// only usable for a single invocation of Analyze and must not be
// reused.
type Config struct {
	// Mains contains the set of 'main' packages to analyze
	// Clients must provide the analysis with at least one
	// package defining a main() function.
	//
	// Non-main packages in the ssa.Program that are not
	// dependencies of any main package may still affect the
	// analysis result, because they contribute runtime types and
	// thus methods.
	// TODO(adonovan): investigate whether this is desirable.
	Mains []*ssa.Package

	// Reflection determines whether to handle reflection
	// operators soundly, which is currently rather slow since it
	// causes constraint to be generated during solving
	// proportional to the number of constraint variables, which
	// has not yet been reduced by presolver optimisation.
	Reflection bool

	// BuildCallGraph determines whether to construct a callgraph.
	// If enabled, the graph will be available in Result.CallGraph.
	BuildCallGraph bool

	// The client populates Queries[v] or IndirectQueries[v]
	// for each ssa.Value v of interest, to request that the
	// points-to sets pts(v) or pts(*v) be computed.  If the
	// client needs both points-to sets, v may appear in both
	// maps.
	//
	// (IndirectQueries is typically used for Values corresponding
	// to source-level lvalues, e.g. an *ssa.Global.)
	//
	// The analysis populates the corresponding
	// Result.{Indirect,}Queries map when it creates the pointer
	// variable for v or *v.  Upon completion the client can
	// inspect that map for the results.
	//
	// TODO(adonovan): this API doesn't scale well for batch tools
	// that want to dump the entire solution.  Perhaps optionally
	// populate a map[*ssa.DebugRef]Pointer in the Result, one
	// entry per source expression.
	//
	Queries         map[ssa.Value]struct{}
	IndirectQueries map[ssa.Value]struct{}
	extendedQueries map[ssa.Value][]*extendedQuery

	// If Log is non-nil, log messages are written to it.
	// Logging is extremely verbose.
	Log io.Writer

	//bz: kcfa
	CallSiteSensitive  bool
	//bz: origin-sensitive -> go routine as origin-entry
	Origin             bool
	//bz: shared config by context-sensitive
	K                  int //how many level? the most recent callsite/origin?
	LimitScope         bool  //only apply kcfa to app methods
	DEBUG             bool
}

type track uint32

const (
	trackChan  track = 1 << iota // track 'chan' references
	trackMap                     // track 'map' references
	trackPtr                     // track regular pointers
	trackSlice                   // track slice references

	trackAll = ^track(0)
)

// AddQuery adds v to Config.Queries.
// Precondition: CanPoint(v.Type()).
func (c *Config) AddQuery(v ssa.Value) {
	if !CanPoint(v.Type()) {
		panic(fmt.Sprintf("%s is not a pointer-like value: %s", v, v.Type()))
	}
	if c.Queries == nil {
		c.Queries = make(map[ssa.Value]struct{})
	}
	c.Queries[v] = struct{}{}
}

// AddQuery adds v to Config.IndirectQueries.
// Precondition: CanPoint(v.Type().Underlying().(*types.Pointer).Elem()).
func (c *Config) AddIndirectQuery(v ssa.Value) {
	if c.IndirectQueries == nil {
		c.IndirectQueries = make(map[ssa.Value]struct{})
	}
	if !CanPoint(mustDeref(v.Type())) {
		panic(fmt.Sprintf("%s is not the address of a pointer-like value: %s", v, v.Type()))
	}
	c.IndirectQueries[v] = struct{}{}
}

// AddExtendedQuery adds an extended, AST-based query on v to the
// analysis. The query, which must be a single Go expression, allows
// destructuring the value.
//
// The query must operate on a variable named 'x', which represents
// the value, and result in a pointer-like object. Only a subset of
// Go expressions are permitted in queries, namely channel receives,
// pointer dereferences, field selectors, array/slice/map/tuple
// indexing and grouping with parentheses. The specific indices when
// indexing arrays, slices and maps have no significance. Indices used
// on tuples must be numeric and within bounds.
//
// All field selectors must be explicit, even ones usually elided
// due to promotion of embedded fields.
//
// The query 'x' is identical to using AddQuery. The query '*x' is
// identical to using AddIndirectQuery.
//
// On success, AddExtendedQuery returns a Pointer to the queried
// value. This Pointer will be initialized during analysis. Using it
// before analysis has finished has undefined behavior.
//
// Example:
// 	// given v, which represents a function call to 'fn() (int, []*T)', and
// 	// 'type T struct { F *int }', the following query will access the field F.
// 	c.AddExtendedQuery(v, "x[1][0].F")
func (c *Config) AddExtendedQuery(v ssa.Value, query string) (*Pointer, error) {
	ops, _, err := parseExtendedQuery(v.Type(), query)
	if err != nil {
		return nil, fmt.Errorf("invalid query %q: %s", query, err)
	}
	if c.extendedQueries == nil {
		c.extendedQueries = make(map[ssa.Value][]*extendedQuery)
	}

	ptr := &Pointer{}
	c.extendedQueries[v] = append(c.extendedQueries[v], &extendedQuery{ops: ops, ptr: ptr})
	return ptr, nil
}

func (c *Config) prog() *ssa.Program {
	for _, main := range c.Mains {
		return main.Prog
	}
	panic("empty scope")
}

type Warning struct {
	Pos     token.Pos
	Message string
}

// A Result contains the results of a pointer analysis.
//
// See Config for how to request the various Result components.
//
type Result struct {
	CallGraph       *callgraph.Graph      // discovered call graph
	Queries         map[ssa.Value]Pointer // pts(v) for each v in Config.Queries.
	IndirectQueries map[ssa.Value]Pointer // pts(*v) for each v in Config.IndirectQueries.
	Warnings        []Warning             // warnings of unsoundness
}

//bz: same as above, but we want contexts
type ResultWCtx struct {
	CallGraph       *GraphWCtx           // discovered call graph
	Queries         map[ssa.Value][]PointerWCtx // pts(v) for each v in setValueNode().
	IndirectQueries map[ssa.Value][]PointerWCtx // pts(*v) for each v in setValueNode().
	GlobalQueries   map[ssa.Value][]PointerWCtx // pts(v) for each freevar in setValueNode().
	Warnings        []Warning                   // warnings of unsoundness
	main            *cgnode          // bz: the cgnode for main method
}

//bz: user API: return *cgnode by *ssa.Function
func (r *ResultWCtx) GetCGNodebyFunc(fn *ssa.Function) []*cgnode {
	return r.CallGraph.Fn2CGNode[fn]
}

//bz: user API: return the main method with type *Node
func (r *ResultWCtx) GetMain() *Node {
	return r.CallGraph.Nodes[r.main]
}

//bz: user API: return []PointerWCtx for a ssa.Value,
//user does not need to distinguish different queries anymore
//input: ssa.Value;
//output: PointerWCtx
//panic: if no record for such input
func (r *ResultWCtx) PointsTo(v ssa.Value) []PointerWCtx {
	pointers := r.Queries[v]
	if pointers != nil {
		return pointers
	}
	pointers = r.IndirectQueries[v]
	if pointers != nil {
		return pointers
	}
	pointers = r.GlobalQueries[v]
	if pointers != nil {
		return pointers
	}
	fmt.Println(" ****  Pointer Analysis did not record for this ssa.Value: " + v.String() + " **** ") //panic
	return nil
}

//bz: user API: for debug to dump all result out
func (r *ResultWCtx) DumpAll() {
	//bz: also a reference of how to use new APIs here
	main := r.GetMain()
	fmt.Println("Main CGNode: " + main.String())

	fmt.Println("\nWe are going to print out call graph. If not desired, turn off DEBUG.")
	callers := r.CallGraph.Nodes
	fmt.Println("#CGNode: " + strconv.Itoa(len(callers)))
	for _, caller := range callers {
		if !strings.Contains(caller.GetFunc().String(), "command-line-arguments.") {
			continue //we only want the app call edges
		}
		fmt.Println(caller.String()) //bz: with context
		outs := caller.Out           // caller --> callee
		for _, out := range outs {   //callees
			fmt.Println("  -> " + out.Callee.String()) //bz: with context
		}
	}

	fmt.Println("\nWe are going to print out queries. If not desired, turn off DEBUG.")
	queries := r.Queries
	inQueries := r.IndirectQueries
	globalQueries := r.GlobalQueries
	fmt.Println("#Queries: " + strconv.Itoa(len(queries)) + "  #Indirect Queries: " + strconv.Itoa(len(inQueries)) +
		"  #Global Queries: " + strconv.Itoa(len(globalQueries)))
	////testing only
	//var p1 pointer.PointerWCtx
	//var p2 pointer.PointerWCtx
	//done := false

	testAPI := false //bz: check for testing new api
	fmt.Println("Queries Detail: ")
	for v, ps := range queries {
		for _, p := range ps { //p -> types.Pointer: includes its context
			//SSA here is your *ssa.Value
			fmt.Println(p.String() + " (SSA:" + v.String() + "): {" + p.PointsTo().String() + "}")
			//if strings.Contains(v.String(), "new bool (abort)") {
			//	p1 = p
			//}
			//if strings.Contains(v.String(), "abort : *bool") {
			//	p2 = p
			//}
		}
		if testAPI {
			check := r.PointsTo(v)
			for _, p := range check { //p -> types.Pointer: includes its context
				fmt.Println(p.String() + " (SSA:" + v.String() + "): {" + p.PointsTo().String() + "}")
			}
		}
	}

	fmt.Println("\nIndirect Queries Detail: ")
	for v, ps := range inQueries {
		for _, p := range ps { //p -> types.Pointer: includes its context
			fmt.Println(p.String() + " (SSA:" + v.String() + "): {" + p.PointsTo().String() + "}")
		}
		if testAPI {
			check := r.PointsTo(v)
			for _, p := range check { //p -> types.Pointer: includes its context
				fmt.Println(p.String() + " (SSA:" + v.String() + "): {" + p.PointsTo().String() + "}")
			}
		}
	}

	fmt.Println("\nGlobal Queries Detail: ")
	for v, ps := range globalQueries {
		for _, p := range ps { //p -> types.Pointer: includes its context
			fmt.Println(p.String() + " (SSA:" + v.String() + "): {" + p.PointsTo().String() + "}")
			//if strings.Contains(v.String(), "abort : *bool") {
			//	p2 = p
			//}
		}
		if testAPI {
			check := r.PointsTo(v)
			for _, p := range check { //p -> types.Pointer: includes its context
				fmt.Println(p.String() + " (SSA:" + v.String() + "): {" + p.PointsTo().String() + "}")
			}
		}
	}
	////testing only
	//yes := p1.PointsTo().Intersects(p2.PointsTo())
	//if yes {
	//	fmt.Println(" @@@@ they intersect @@@@ ")
	//}
}

// A Pointer is an equivalence class of pointer-like values.
//
// A Pointer doesn't have a unique type because pointers of distinct
// types may alias the same object.
//
type Pointer struct {
	a *analysis
	n nodeid
}

// A PointsToSet is a set of labels (locations or allocations).
type PointsToSet struct {
	a   *analysis // may be nil if pts is nil
	pts *nodeset
}

func (s PointsToSet) String() string {
	var buf bytes.Buffer
	buf.WriteByte('[')
	if s.pts != nil {
		var space [50]int
		for i, l := range s.pts.AppendTo(space[:0]) {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(s.a.labelFor(nodeid(l)).String())
		}
	}
	buf.WriteByte(']')
	return buf.String()
}

// PointsTo returns the set of labels that this points-to set
// contains.
func (s PointsToSet) Labels() []*Label {
	var labels []*Label
	if s.pts != nil {
		var space [50]int
		for _, l := range s.pts.AppendTo(space[:0]) {
			labels = append(labels, s.a.labelFor(nodeid(l)))
		}
	}
	return labels
}

// If this PointsToSet came from a Pointer of interface kind
// or a reflect.Value, DynamicTypes returns the set of dynamic
// types that it may contain.  (For an interface, they will
// always be concrete types.)
//
// The result is a mapping whose keys are the dynamic types to which
// it may point.  For each pointer-like key type, the corresponding
// map value is the PointsToSet for pointers of that type.
//
// The result is empty unless CanHaveDynamicTypes(T).
//
func (s PointsToSet) DynamicTypes() *typeutil.Map {
	var tmap typeutil.Map
	tmap.SetHasher(s.a.hasher)
	if s.pts != nil {
		var space [50]int
		for _, x := range s.pts.AppendTo(space[:0]) {
			ifaceObjID := nodeid(x)
			if !s.a.isTaggedObject(ifaceObjID) {
				continue // !CanHaveDynamicTypes(tDyn)
			}
			tDyn, v, indirect := s.a.taggedValue(ifaceObjID)
			if indirect {
				panic("indirect tagged object") // implement later
			}
			pts, ok := tmap.At(tDyn).(PointsToSet)
			if !ok {
				pts = PointsToSet{s.a, new(nodeset)}
				tmap.Set(tDyn, pts)
			}
			pts.pts.addAll(&s.a.nodes[v].solve.pts)
		}
	}
	return &tmap
}

// Intersects reports whether this points-to set and the
// argument points-to set contain common members.
func (s PointsToSet) Intersects(y PointsToSet) bool {
	if s.pts == nil || y.pts == nil {
		return false
	}
	// This takes Θ(|x|+|y|) time.
	var z intsets.Sparse
	z.Intersection(&s.pts.Sparse, &y.pts.Sparse)
	return !z.IsEmpty()
}

func (p Pointer) String() string {
	return fmt.Sprintf("n%d", p.n)
}

// PointsTo returns the points-to set of this pointer.
func (p Pointer) PointsTo() PointsToSet {
	if p.n == 0 {
		return PointsToSet{}
	}
	return PointsToSet{p.a, &p.a.nodes[p.n].solve.pts}
}

// MayAlias reports whether the receiver pointer may alias
// the argument pointer.
func (p Pointer) MayAlias(q Pointer) bool {
	return p.PointsTo().Intersects(q.PointsTo())
}

// DynamicTypes returns p.PointsTo().DynamicTypes().
func (p Pointer) DynamicTypes() *typeutil.Map {
	return p.PointsTo().DynamicTypes()
}


// bz: a Pointer with context
type PointerWCtx struct {
	a     *analysis
	n     nodeid
	cgn   *cgnode
}

//bz: whether goID is match with the contexts in this pointer
//TODO: this does not match parent context if callsite.length > 1 (k > 1)
func (p PointerWCtx) MatchMyContext(go_instr *ssa.Go) bool {
	my_go_instr := p.cgn.callersite[0].goInstr
	if my_go_instr == go_instr {
		return true
	}else{
		return false
	}
}

//bz: return the context of cgn which calls setValueNode() to record this pointer;
func (p PointerWCtx) GetMyContext() []*callsite {
	return p.cgn.callersite
}

//bz: return the cgn which calls setValueNode(); ctx is inside
func (p PointerWCtx) Parent() *cgnode {
	return p.cgn
}

//bz: add ctx
func (p PointerWCtx) String() string {
	if p.cgn == nil {
		return fmt.Sprintf("n%d&Global", p.n)
	}
	return fmt.Sprintf("n%d&%s", p.n, p.cgn.contourkFull())
}

// PointsTo returns the points-to set of this pointer.
func (p PointerWCtx) PointsTo() PointsToSet {
	if p.n == 0 {
		return PointsToSet{}
	}
	return PointsToSet{p.a, &p.a.nodes[p.n].solve.pts}
}

// MayAlias reports whether the receiver pointer may alias
// the argument pointer.  --> same as Intersects()
func (p PointerWCtx) MayAlias(q PointerWCtx) bool {
	return p.PointsTo().Intersects(q.PointsTo())
}

// DynamicTypes returns p.PointsTo().DynamicTypes().
func (p PointerWCtx) DynamicTypes() *typeutil.Map {
	return p.PointsTo().DynamicTypes()
}

// PointsTo returns the set of labels that this points-to set
// contains. -- TODO: bz: is this working??
func (p PointerWCtx) Labels() []*Label {
	var labels []*Label
	var pp = p.PointsTo()
	var pts = pp.pts
	if pts != nil {
		var space [50]int
		for _, l := range pts.AppendTo(space[:0]) {
			labels = append(labels, pp.a.labelFor(nodeid(l)))
		}
	}
	return labels
}
