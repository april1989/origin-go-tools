package compare

import (
	"fmt"
	"github.tamu.edu/April1989/go_tools/go/pointer"
)

//compute the common paths in a set of mains from a pkg

var cands []*pointer.ResultWCtx

func AddCandidate(res *pointer.ResultWCtx) {
	cands = append(cands, res)
}

//compute common parts among all candidates
func ComputeCommonParts() {
	fmt.Println("\n\nCompute Common part ... ") //only shared contours




}