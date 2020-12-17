package b

import (
	_ "github.com/o2lab/go-tools/internal/lsp/circular/double/one" //@diag("_ \"github.com/o2lab/go-tools/internal/lsp/circular/double/one\"", "compiler", "import cycle not allowed", "error"),diag("\"github.com/o2lab/go-tools/internal/lsp/circular/double/one\"", "compiler", "could not import github.com/o2lab/go-tools/internal/lsp/circular/double/one (no package for import github.com/o2lab/go-tools/internal/lsp/circular/double/one)", "error")
)
