package errors

import (
	"github.com/o2lab/go-tools/internal/lsp/types"
)

func _() {
	bob.Bob() //@complete(".")
	types.b //@complete(" //", Bob_interface)
}
