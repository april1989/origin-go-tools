// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lsp

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/april1989/origin-go-tools/internal/jsonrpc2"
	"github.com/april1989/origin-go-tools/internal/lsp/protocol"
	"github.com/april1989/origin-go-tools/internal/lsp/source"
	"github.com/april1989/origin-go-tools/internal/span"
	errors "golang.org/x/xerrors"
)

// ModificationSource identifies the originating cause of a file modification.
type ModificationSource int

const (
	// FromDidOpen is a file modification caused by opening a file.
	FromDidOpen = ModificationSource(iota)

	// FromDidChange is a file modification caused by changing a file.
	FromDidChange

	// FromDidChangeWatchedFiles is a file modification caused by a change to a
	// watched file.
	FromDidChangeWatchedFiles

	// FromDidSave is a file modification caused by a file save.
	FromDidSave

	// FromDidClose is a file modification caused by closing a file.
	FromDidClose

	// FromRegenerateCgo refers to file modifications caused by regenerating
	// the cgo sources for the workspace.
	FromRegenerateCgo

	// FromInitialWorkspaceLoad refers to the loading of all packages in the
	// workspace when the view is first created.
	FromInitialWorkspaceLoad
)

func (m ModificationSource) String() string {
	switch m {
	case FromDidOpen:
		return "opened files"
	case FromDidChange:
		return "changed files"
	case FromDidChangeWatchedFiles:
		return "files changed on disk"
	case FromDidSave:
		return "saved files"
	case FromDidClose:
		return "close files"
	case FromRegenerateCgo:
		return "regenerate cgo"
	case FromInitialWorkspaceLoad:
		return "initial workspace load"
	default:
		return "unknown file modification"
	}
}

func (s *Server) didOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	uri := params.TextDocument.URI.SpanURI()
	if !uri.IsFile() {
		return nil
	}
	// There may not be any matching view in the current session. If that's
	// the case, try creating a new view based on the opened file path.
	//
	// TODO(rstambler): This seems like it would continuously add new
	// views, but it won't because ViewOf only returns an error when there
	// are no views in the session. I don't know if that logic should go
	// here, or if we can continue to rely on that implementation detail.
	if _, err := s.session.ViewOf(uri); err != nil {
		dir := filepath.Dir(uri.Filename())
		if err := s.addFolders(ctx, []protocol.WorkspaceFolder{{
			URI:  string(protocol.URIFromPath(dir)),
			Name: filepath.Base(dir),
		}}); err != nil {
			return err
		}
	}

	return s.didModifyFiles(ctx, []source.FileModification{
		{
			URI:        uri,
			Action:     source.Open,
			Version:    params.TextDocument.Version,
			Text:       []byte(params.TextDocument.Text),
			LanguageID: params.TextDocument.LanguageID,
		},
	}, FromDidOpen)
}

func (s *Server) didChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	uri := params.TextDocument.URI.SpanURI()
	if !uri.IsFile() {
		return nil
	}

	text, err := s.changedText(ctx, uri, params.ContentChanges)
	if err != nil {
		return err
	}
	c := source.FileModification{
		URI:     uri,
		Action:  source.Change,
		Version: params.TextDocument.Version,
		Text:    text,
	}
	if err := s.didModifyFiles(ctx, []source.FileModification{c}, FromDidChange); err != nil {
		return err
	}

	s.changedFilesMu.Lock()
	defer s.changedFilesMu.Unlock()

	s.changedFiles[uri] = struct{}{}
	return nil
}

func (s *Server) didChangeWatchedFiles(ctx context.Context, params *protocol.DidChangeWatchedFilesParams) error {
	var modifications []source.FileModification
	for _, change := range params.Changes {
		uri := change.URI.SpanURI()
		if !uri.IsFile() {
			continue
		}
		action := changeTypeToFileAction(change.Type)
		modifications = append(modifications, source.FileModification{
			URI:    uri,
			Action: action,
			OnDisk: true,
		})
	}
	return s.didModifyFiles(ctx, modifications, FromDidChangeWatchedFiles)
}

func (s *Server) didSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) error {
	uri := params.TextDocument.URI.SpanURI()
	if !uri.IsFile() {
		return nil
	}
	c := source.FileModification{
		URI:     uri,
		Action:  source.Save,
		Version: params.TextDocument.Version,
	}
	if params.Text != nil {
		c.Text = []byte(*params.Text)
	}
	return s.didModifyFiles(ctx, []source.FileModification{c}, FromDidSave)
}

func (s *Server) didClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := params.TextDocument.URI.SpanURI()
	if !uri.IsFile() {
		return nil
	}
	return s.didModifyFiles(ctx, []source.FileModification{
		{
			URI:     uri,
			Action:  source.Close,
			Version: -1,
			Text:    nil,
		},
	}, FromDidClose)
}

func (s *Server) didModifyFiles(ctx context.Context, modifications []source.FileModification, cause ModificationSource) error {
	// diagnosticWG tracks outstanding diagnostic work as a result of this file
	// modification.
	var diagnosticWG sync.WaitGroup
	if s.session.Options().VerboseWorkDoneProgress {
		work := s.progress.start(ctx, DiagnosticWorkTitle(cause), "Calculating file diagnostics...", nil, nil)
		defer func() {
			go func() {
				diagnosticWG.Wait()
				work.end("Done.")
			}()
		}()
	}
	snapshots, releases, deletions, err := s.session.DidModifyFiles(ctx, modifications)
	if err != nil {
		return err
	}

	for _, uri := range deletions {
		if err := s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
			URI:         protocol.URIFromSpanURI(uri),
			Diagnostics: []protocol.Diagnostic{},
			Version:     0,
		}); err != nil {
			return err
		}
	}
	snapshotByURI := make(map[span.URI]source.Snapshot)
	for _, c := range modifications {
		snapshotByURI[c.URI] = nil
	}
	// Avoid diagnosing the same snapshot twice.
	snapshotSet := make(map[source.Snapshot][]span.URI)
	for uri := range snapshotByURI {
		view, err := s.session.ViewOf(uri)
		if err != nil {
			return err
		}
		var snapshot source.Snapshot
		for _, s := range snapshots {
			if s.View() == view {
				if snapshot != nil {
					return errors.Errorf("duplicate snapshots for the same view")
				}
				snapshot = s
			}
		}
		// If the file isn't in any known views (for example, if it's in a dependency),
		// we may not have a snapshot to map it to. As a result, we won't try to
		// diagnose it. TODO(rstambler): Figure out how to handle this better.
		if snapshot == nil {
			continue
		}
		snapshotSet[snapshot] = append(snapshotSet[snapshot], uri)
		snapshotByURI[uri] = snapshot
	}

	for _, mod := range modifications {
		if mod.OnDisk || mod.Action != source.Change {
			continue
		}
		snapshot, ok := snapshotByURI[mod.URI]
		if !ok {
			continue
		}
		// Ideally, we should be able to specify that a generated file should be opened as read-only.
		// Tell the user that they should not be editing a generated file.
		if s.wasFirstChange(mod.URI) && source.IsGenerated(ctx, snapshot, mod.URI) {
			if err := s.client.ShowMessage(ctx, &protocol.ShowMessageParams{
				Message: fmt.Sprintf("Do not edit this file! %s is a generated file.", mod.URI.Filename()),
				Type:    protocol.Warning,
			}); err != nil {
				return err
			}
		}
	}

	for snapshot, uris := range snapshotSet {
		// If a modification comes in for the view's go.mod file and the view
		// was never properly initialized, or the view does not have
		// a go.mod file, try to recreate the associated view.
		if modfile := snapshot.View().ModFile(); modfile == "" {
			for _, uri := range uris {
				// Don't rebuild the view until the go.mod is on disk.
				if !snapshot.IsSaved(uri) {
					continue
				}
				fh, err := snapshot.GetFile(ctx, uri)
				if err != nil {
					return err
				}
				switch fh.Kind() {
				case source.Mod:
					newSnapshot, release, err := snapshot.View().Rebuild(ctx)
					releases = append(releases, release)
					if err != nil {
						return err
					}
					// Update the snapshot to the rebuilt one.
					snapshot = newSnapshot
				}
			}
		}
		diagnosticWG.Add(1)
		go func(snapshot source.Snapshot) {
			defer diagnosticWG.Done()
			s.diagnoseSnapshot(snapshot)
		}(snapshot)
	}

	go func() {
		diagnosticWG.Wait()
		for _, release := range releases {
			release()
		}
	}()
	// After any file modifications, we need to update our watched files,
	// in case something changed. Compute the new set of directories to watch,
	// and if it differs from the current set, send updated registrations.
	if err := s.updateWatchedDirectories(ctx, snapshots); err != nil {
		return err
	}
	return nil
}

// DiagnosticWorkTitle returns the title of the diagnostic work resulting from a
// file change originating from the given cause.
func DiagnosticWorkTitle(cause ModificationSource) string {
	return fmt.Sprintf("diagnosing %v", cause)
}

func (s *Server) wasFirstChange(uri span.URI) bool {
	s.changedFilesMu.Lock()
	defer s.changedFilesMu.Unlock()

	if s.changedFiles == nil {
		s.changedFiles = make(map[span.URI]struct{})
	}
	_, ok := s.changedFiles[uri]
	return !ok
}

func (s *Server) changedText(ctx context.Context, uri span.URI, changes []protocol.TextDocumentContentChangeEvent) ([]byte, error) {
	if len(changes) == 0 {
		return nil, errors.Errorf("%w: no content changes provided", jsonrpc2.ErrInternal)
	}

	// Check if the client sent the full content of the file.
	// We accept a full content change even if the server expected incremental changes.
	if len(changes) == 1 && changes[0].Range == nil && changes[0].RangeLength == 0 {
		return []byte(changes[0].Text), nil
	}
	return s.applyIncrementalChanges(ctx, uri, changes)
}

func (s *Server) applyIncrementalChanges(ctx context.Context, uri span.URI, changes []protocol.TextDocumentContentChangeEvent) ([]byte, error) {
	fh, err := s.session.GetFile(ctx, uri)
	if err != nil {
		return nil, err
	}
	content, err := fh.Read()
	if err != nil {
		return nil, errors.Errorf("%w: file not found (%v)", jsonrpc2.ErrInternal, err)
	}
	for _, change := range changes {
		// Make sure to update column mapper along with the content.
		converter := span.NewContentConverter(uri.Filename(), content)
		m := &protocol.ColumnMapper{
			URI:       uri,
			Converter: converter,
			Content:   content,
		}
		if change.Range == nil {
			return nil, errors.Errorf("%w: unexpected nil range for change", jsonrpc2.ErrInternal)
		}
		spn, err := m.RangeSpan(*change.Range)
		if err != nil {
			return nil, err
		}
		if !spn.HasOffset() {
			return nil, errors.Errorf("%w: invalid range for content change", jsonrpc2.ErrInternal)
		}
		start, end := spn.Start().Offset(), spn.End().Offset()
		if end < start {
			return nil, errors.Errorf("%w: invalid range for content change", jsonrpc2.ErrInternal)
		}
		var buf bytes.Buffer
		buf.Write(content[:start])
		buf.WriteString(change.Text)
		buf.Write(content[end:])
		content = buf.Bytes()
	}
	return content, nil
}

func changeTypeToFileAction(ct protocol.FileChangeType) source.FileAction {
	switch ct {
	case protocol.Changed:
		return source.Change
	case protocol.Created:
		return source.Create
	case protocol.Deleted:
		return source.Delete
	}
	return source.UnknownFileAction
}
