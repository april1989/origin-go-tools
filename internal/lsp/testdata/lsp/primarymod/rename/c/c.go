package c

import "github.com/o2lab/go-tools/internal/lsp/rename/b"

func _() {
	b.Hello() //@rename("Hello", "Goodbye")
}
