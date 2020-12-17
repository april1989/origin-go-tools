package danglingstmt

import "github.com/o2lab/go-tools/internal/lsp/foo"

func _() {
	foo. //@rank(" //", Foo)
	var _ = []string{foo.} //@rank("}", Foo)
}
