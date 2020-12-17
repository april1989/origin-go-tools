package a

import (
	_ "github.com/o2lab/go-tools/internal/lsp/circular/triple/b" //@diag("_ \"github.com/o2lab/go-tools/internal/lsp/circular/triple/b\"", "compiler", "import cycle not allowed", "error")
)
