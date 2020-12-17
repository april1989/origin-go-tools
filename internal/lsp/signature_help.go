// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lsp

import (
	"context"

	"github.com/o2lab/go-tools/internal/event"
	"github.com/o2lab/go-tools/internal/lsp/debug/tag"
	"github.com/o2lab/go-tools/internal/lsp/protocol"
	"github.com/o2lab/go-tools/internal/lsp/source"
)

func (s *Server) signatureHelp(ctx context.Context, params *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	snapshot, fh, ok, release, err := s.beginFileRequest(ctx, params.TextDocument.URI, source.Go)
	defer release()
	if !ok {
		return nil, err
	}
	info, activeParameter, err := source.SignatureHelp(ctx, snapshot, fh, params.Position)
	if err != nil {
		event.Error(ctx, "no signature help", err, tag.Position.Of(params.Position))
		return nil, nil
	}
	return &protocol.SignatureHelp{
		Signatures:      []protocol.SignatureInformation{*info},
		ActiveParameter: float64(activeParameter),
	}, nil
}
