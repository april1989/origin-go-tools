package compare

import (
	"fmt"
	"github.tamu.edu/April1989/go_tools/go/pointer"
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
func ComputeCommonParts() {
	fmt.Println("\n\nCompute Common parts ... ") //only shared contours

}
