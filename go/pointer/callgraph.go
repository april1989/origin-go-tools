// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pointer

// This file defines the internal (context-sensitive) call graph.

import (
	"fmt"
	"go/token"
	"strconv"

	"golang.org/x/tools/go/ssa"
)

//bz: this is the cgnode created by pointer analysis -> we force to use k callersite
type cgnode struct {
	fn         *ssa.Function
	obj        nodeid      // start of this contour's object block
	sites      []*callsite // ordered list of callsites within this function
	callersite []*callsite   // where called from, if known; nil for shared contours ----> bz: k-caller site
}

// contour returns a description of this node's contour.
//bz: only used for log
func (n *cgnode) contour(isKcfa bool) string {
	if isKcfa {//bz: print out info for kcfa
		return n.contourkFull()
	}
	//bz: adjust for context-insensitive. Same as 1callsite, only showing the most recent callersite info
	if n.callersite == nil || len(n.callersite) == 0 || n.callersite[0] == nil{
		return "shared contour"
	}
	if n.callersite[0].instr != nil {
		return fmt.Sprintf("as called from %s", n.callersite[0].instr.Parent())
	}
	return fmt.Sprintf("as called to synthetic/intrinsic (targets=n%d)", n.callersite[0].targets)
}

//bz: adjust contour() to kcfa
func (n *cgnode) contourkFull() string {
	var s string
	s = s +  "["
	for idx, cs := range n.callersite {
		if cs == nil {
		    s = s + strconv.Itoa(idx) + ":shared contour; "
		    continue
		}
		if cs.instr != nil {
			s = s  + strconv.Itoa(idx) + ":" + cs.instr.String() + "@" + cs.instr.Parent().String() + "; "
			continue
		}
		if cs.targets == 2 { //bz: the ctx is "called to synthetic/intrinsic func@n2"; which is root node calling to main.main
			s = s + strconv.Itoa(idx) + ":root call to command-line-arguments.main"
			continue
		}
		s = s + strconv.Itoa(idx) + ":" + "called to synthetic/intrinsic func@" + cs.targets.String() + "; "
	}
	s = s + "]"
	return s
}

func (n *cgnode) String() string {
	return fmt.Sprintf("cg%d:%s%s", n.obj, n.fn, n.contourkFull())
}

// A callsite represents a single call site within a cgnode;
// it is implicitly context-sensitive.
// callsites never represent calls to built-ins;
// they are handled as intrinsics.
// bz: this is the call site we used
type callsite struct {
	targets nodeid              // pts(·) contains objects for dynamically called functions
	instr   ssa.CallInstruction // the call instruction; nil for synthetic/intrinsic
}

//bz: to see if two callsites are the same
//tmp solution, to compare string ... otherwise too strict ...
//e.g., return c.targets == other.targets && c.instr.String() == other.instr.String() ----->  this might be too strict ...
func (c *callsite) equal(o *callsite) bool {
	if o == nil { return false }
	cInstr := c.instr
	oInstr := o.instr
	if cInstr == nil && oInstr == nil {
		return c.targets == o.targets
	}else if cInstr == nil || oInstr == nil {
		return false //one is k callsite, one is from closure
	}else{ // most cases, comparing between callsite and callsite
		return cInstr.String() == oInstr.String() && cInstr.Parent().String() == oInstr.Parent().String()
	}
}


func (c *callsite) String() string {
	if c.instr != nil {
		//return c.instr.Common().Description() //bz: original code
		return c.instr.String() + "@" + c.instr.Parent().String()
	}
	return "synthetic function call@" + c.targets.String()
}

// pos returns the source position of this callsite, or token.NoPos if implicit.
func (c *callsite) pos() token.Pos {
	if c.instr != nil {
		return c.instr.Pos()
	}
	return token.NoPos
}


//bz: to record the 1callsite from caller to nodeid for makeclosure, work together with a.closures[]
//TODO: full callchain ??
type Ctx2nodeid struct {
	ctx2nodeid map[*callsite]nodeid
}

