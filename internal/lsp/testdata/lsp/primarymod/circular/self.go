package circular

import (
	_ "github.com/o2lab/go-tools/internal/lsp/circular" //@diag("_ \"github.com/o2lab/go-tools/internal/lsp/circular\"", "compiler", "import cycle not allowed", "error"),diag("\"github.com/o2lab/go-tools/internal/lsp/circular\"", "compiler", "could not import github.com/o2lab/go-tools/internal/lsp/circular (no package for import github.com/o2lab/go-tools/internal/lsp/circular)", "error")
)
