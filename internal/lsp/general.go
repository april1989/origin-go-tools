// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lsp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/april1989/origin-go-tools/internal/event"
	"github.com/april1989/origin-go-tools/internal/jsonrpc2"
	"github.com/april1989/origin-go-tools/internal/lsp/debug"
	"github.com/april1989/origin-go-tools/internal/lsp/debug/tag"
	"github.com/april1989/origin-go-tools/internal/lsp/protocol"
	"github.com/april1989/origin-go-tools/internal/lsp/source"
	"github.com/april1989/origin-go-tools/internal/span"
	errors "golang.org/x/xerrors"
)

func (s *Server) initialize(ctx context.Context, params *protocol.ParamInitialize) (*protocol.InitializeResult, error) {
	s.stateMu.Lock()
	if s.state >= serverInitializing {
		defer s.stateMu.Unlock()
		return nil, errors.Errorf("%w: initialize called while server in %v state", jsonrpc2.ErrInvalidRequest, s.state)
	}
	s.state = serverInitializing
	s.stateMu.Unlock()

	s.progress.supportsWorkDoneProgress = params.Capabilities.Window.WorkDoneProgress

	options := s.session.Options()
	defer func() { s.session.SetOptions(options) }()

	if err := s.handleOptionResults(ctx, source.SetOptions(&options, params.InitializationOptions)); err != nil {
		return nil, err
	}
	options.ForClientCapabilities(params.Capabilities)

	folders := params.WorkspaceFolders
	if len(folders) == 0 {
		if params.RootURI != "" {
			folders = []protocol.WorkspaceFolder{{
				URI:  string(params.RootURI),
				Name: path.Base(params.RootURI.SpanURI().Filename()),
			}}
		}
	}
	for _, folder := range folders {
		uri := span.URIFromURI(folder.URI)
		if !uri.IsFile() {
			continue
		}
		s.pendingFolders = append(s.pendingFolders, folder)
	}
	// gopls only supports URIs with a file:// scheme, so if we have no
	// workspace folders with a supported scheme, fail to initialize.
	if len(folders) > 0 && len(s.pendingFolders) == 0 {
		return nil, fmt.Errorf("unsupported URI schemes: %v (gopls only supports file URIs)", folders)
	}

	var codeActionProvider interface{} = true
	if ca := params.Capabilities.TextDocument.CodeAction; len(ca.CodeActionLiteralSupport.CodeActionKind.ValueSet) > 0 {
		// If the client has specified CodeActionLiteralSupport,
		// send the code actions we support.
		//
		// Using CodeActionOptions is only valid if codeActionLiteralSupport is set.
		codeActionProvider = &protocol.CodeActionOptions{
			CodeActionKinds: s.getSupportedCodeActions(),
		}
	}
	var renameOpts interface{} = true
	if r := params.Capabilities.TextDocument.Rename; r.PrepareSupport {
		renameOpts = protocol.RenameOptions{
			PrepareProvider: r.PrepareSupport,
		}
	}

	goplsVer := &bytes.Buffer{}
	debug.PrintVersionInfo(ctx, goplsVer, true, debug.PlainText)

	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			CallHierarchyProvider: true,
			CodeActionProvider:    codeActionProvider,
			CompletionProvider: protocol.CompletionOptions{
				TriggerCharacters: []string{"."},
			},
			DefinitionProvider:         true,
			TypeDefinitionProvider:     true,
			ImplementationProvider:     true,
			DocumentFormattingProvider: true,
			DocumentSymbolProvider:     true,
			WorkspaceSymbolProvider:    true,
			ExecuteCommandProvider: protocol.ExecuteCommandOptions{
				Commands: options.SupportedCommands,
			},
			FoldingRangeProvider:      true,
			HoverProvider:             true,
			DocumentHighlightProvider: true,
			DocumentLinkProvider:      protocol.DocumentLinkOptions{},
			ReferencesProvider:        true,
			RenameProvider:            renameOpts,
			SignatureHelpProvider: protocol.SignatureHelpOptions{
				TriggerCharacters: []string{"(", ","},
			},
			TextDocumentSync: &protocol.TextDocumentSyncOptions{
				Change:    protocol.Incremental,
				OpenClose: true,
				Save: protocol.SaveOptions{
					IncludeText: false,
				},
			},
			Workspace: protocol.WorkspaceGn{
				WorkspaceFolders: protocol.WorkspaceFoldersGn{
					Supported:           true,
					ChangeNotifications: "workspace/didChangeWorkspaceFolders",
				},
			},
		},
		ServerInfo: struct {
			Name    string `json:"name"`
			Version string `json:"version,omitempty"`
		}{
			Name:    "gopls",
			Version: goplsVer.String(),
		},
	}, nil
}

func (s *Server) initialized(ctx context.Context, params *protocol.InitializedParams) error {
	s.stateMu.Lock()
	if s.state >= serverInitialized {
		defer s.stateMu.Unlock()
		return errors.Errorf("%w: initalized called while server in %v state", jsonrpc2.ErrInvalidRequest, s.state)
	}
	s.state = serverInitialized
	s.stateMu.Unlock()

	options := s.session.Options()
	defer func() { s.session.SetOptions(options) }()

	// TODO: this event logging may be unnecessary.
	// The version info is included in the initialize response.
	buf := &bytes.Buffer{}
	debug.PrintVersionInfo(ctx, buf, true, debug.PlainText)
	event.Log(ctx, buf.String())

	if err := s.addFolders(ctx, s.pendingFolders); err != nil {
		return err
	}
	s.pendingFolders = nil

	if options.ConfigurationSupported && options.DynamicConfigurationSupported {
		if err := s.client.RegisterCapability(ctx, &protocol.RegistrationParams{
			Registrations: []protocol.Registration{
				{
					ID:     "workspace/didChangeConfiguration",
					Method: "workspace/didChangeConfiguration",
				},
				{
					ID:     "workspace/didChangeWorkspaceFolders",
					Method: "workspace/didChangeWorkspaceFolders",
				},
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) addFolders(ctx context.Context, folders []protocol.WorkspaceFolder) error {
	originalViews := len(s.session.Views())
	viewErrors := make(map[span.URI]error)

	var wg sync.WaitGroup
	if s.session.Options().VerboseWorkDoneProgress {
		work := s.progress.start(ctx, DiagnosticWorkTitle(FromInitialWorkspaceLoad), "Calculating diagnostics for initial workspace load...", nil, nil)
		defer func() {
			go func() {
				wg.Wait()
				work.end("Done.")
			}()
		}()
	}
	dirsToWatch := map[span.URI]struct{}{}
	for _, folder := range folders {
		uri := span.URIFromURI(folder.URI)
		// Ignore non-file URIs.
		if !uri.IsFile() {
			continue
		}
		work := s.progress.start(ctx, "Setting up workspace", "Loading packages...", nil, nil)
		view, snapshot, release, err := s.addView(ctx, folder.Name, uri)
		if err != nil {
			viewErrors[uri] = err
			work.end(fmt.Sprintf("Error loading packages: %s", err))
			continue
		}
		go func() {
			view.AwaitInitialized(ctx)
			work.end("Finished loading packages.")
		}()

		for _, dir := range snapshot.WorkspaceDirectories(ctx) {
			dirsToWatch[dir] = struct{}{}
		}

		// Print each view's environment.
		buf := &bytes.Buffer{}
		if err := view.WriteEnv(ctx, buf); err != nil {
			event.Error(ctx, "failed to write environment", err, tag.Directory.Of(view.Folder().Filename()))
			continue
		}
		event.Log(ctx, buf.String())

		// Diagnose the newly created view.
		wg.Add(1)
		go func() {
			s.diagnoseDetached(snapshot)
			release()
			wg.Done()
		}()
	}
	// Register for file watching notifications, if they are supported.
	s.watchedDirectoriesMu.Lock()
	err := s.registerWatchedDirectoriesLocked(ctx, dirsToWatch)
	s.watchedDirectoriesMu.Unlock()
	if err != nil {
		return err
	}
	if len(viewErrors) > 0 {
		errMsg := fmt.Sprintf("Error loading workspace folders (expected %v, got %v)\n", len(folders), len(s.session.Views())-originalViews)
		for uri, err := range viewErrors {
			errMsg += fmt.Sprintf("failed to load view for %s: %v\n", uri, err)
		}
		return s.client.ShowMessage(ctx, &protocol.ShowMessageParams{
			Type:    protocol.Error,
			Message: errMsg,
		})
	}
	return nil
}

// updateWatchedDirectories compares the current set of directories to watch
// with the previously registered set of directories. If the set of directories
// has changed, we unregister and re-register for file watching notifications.
// updatedSnapshots is the set of snapshots that have been updated.
func (s *Server) updateWatchedDirectories(ctx context.Context, updatedSnapshots []source.Snapshot) error {
	dirsToWatch := map[span.URI]struct{}{}
	seenViews := map[source.View]struct{}{}

	// Collect all of the workspace directories from the updated snapshots.
	for _, snapshot := range updatedSnapshots {
		seenViews[snapshot.View()] = struct{}{}
		for _, dir := range snapshot.WorkspaceDirectories(ctx) {
			dirsToWatch[dir] = struct{}{}
		}
	}
	// Not all views were necessarily updated, so check the remaining views.
	for _, view := range s.session.Views() {
		if _, ok := seenViews[view]; ok {
			continue
		}
		snapshot, release := view.Snapshot(ctx)
		for _, dir := range snapshot.WorkspaceDirectories(ctx) {
			dirsToWatch[dir] = struct{}{}
		}
		release()
	}

	s.watchedDirectoriesMu.Lock()
	defer s.watchedDirectoriesMu.Unlock()

	// Nothing to do if the set of workspace directories is unchanged.
	if equalURISet(s.watchedDirectories, dirsToWatch) {
		return nil
	}

	// If the set of directories to watch has changed, register the updates and
	// unregister the previously watched directories. This ordering avoids a
	// period where no files are being watched. Still, if a user makes on-disk
	// changes before these updates are complete, we may miss them for the new
	// directories.
	if s.watchRegistrationCount > 0 {
		prevID := s.watchRegistrationCount - 1
		if err := s.registerWatchedDirectoriesLocked(ctx, dirsToWatch); err != nil {
			return err
		}
		return s.client.UnregisterCapability(ctx, &protocol.UnregistrationParams{
			Unregisterations: []protocol.Unregistration{{
				ID:     watchedFilesCapabilityID(prevID),
				Method: "workspace/didChangeWatchedFiles",
			}},
		})
	}
	return nil
}

func watchedFilesCapabilityID(id uint64) string {
	return fmt.Sprintf("workspace/didChangeWatchedFiles-%d", id)
}

func equalURISet(m1, m2 map[span.URI]struct{}) bool {
	if len(m1) != len(m2) {
		return false
	}
	for k := range m1 {
		_, ok := m2[k]
		if !ok {
			return false
		}
	}
	return true
}

// registerWatchedDirectoriesLocked sends the workspace/didChangeWatchedFiles
// registrations to the client and updates s.watchedDirectories.
func (s *Server) registerWatchedDirectoriesLocked(ctx context.Context, dirs map[span.URI]struct{}) error {
	if !s.session.Options().DynamicWatchedFilesSupported {
		return nil
	}
	for k := range s.watchedDirectories {
		delete(s.watchedDirectories, k)
	}
	// Work-around microsoft/vscode#100870 by making sure that we are,
	// at least, watching the user's entire workspace. This will still be
	// applied to every folder in the workspace.
	watchers := []protocol.FileSystemWatcher{{
		GlobPattern: "**/*.{go,mod,sum}",
		Kind:        float64(protocol.WatchChange + protocol.WatchDelete + protocol.WatchCreate),
	}}
	for dir := range dirs {
		filename := dir.Filename()
		// If the directory is within a workspace folder, we're already
		// watching it via the relative path above.
		for _, view := range s.session.Views() {
			if isSubdirectory(view.Folder().Filename(), filename) {
				continue
			}
		}
		// If microsoft/vscode#100870 is resolved before
		// microsoft/vscode#104387, we will need a work-around for Windows
		// drive letter casing.
		watchers = append(watchers, protocol.FileSystemWatcher{
			GlobPattern: fmt.Sprintf("%s/**/*.{go,mod,sum}", filename),
			Kind:        float64(protocol.WatchChange + protocol.WatchDelete + protocol.WatchCreate),
		})
	}
	if err := s.client.RegisterCapability(ctx, &protocol.RegistrationParams{
		Registrations: []protocol.Registration{{
			ID:     watchedFilesCapabilityID(s.watchRegistrationCount),
			Method: "workspace/didChangeWatchedFiles",
			RegisterOptions: protocol.DidChangeWatchedFilesRegistrationOptions{
				Watchers: watchers,
			},
		}},
	}); err != nil {
		return err
	}
	s.watchRegistrationCount++

	for dir := range dirs {
		s.watchedDirectories[dir] = struct{}{}
	}
	return nil
}

func isSubdirectory(root, leaf string) bool {
	rel, err := filepath.Rel(root, leaf)
	return err == nil && !strings.HasPrefix(rel, "..")
}

func (s *Server) fetchConfig(ctx context.Context, name string, folder span.URI, o *source.Options) error {
	if !s.session.Options().ConfigurationSupported {
		return nil
	}
	v := protocol.ParamConfiguration{
		ConfigurationParams: protocol.ConfigurationParams{
			Items: []protocol.ConfigurationItem{{
				ScopeURI: string(folder),
				Section:  "gopls",
			}, {
				ScopeURI: string(folder),
				Section:  fmt.Sprintf("gopls-%s", name),
			}},
		},
	}
	configs, err := s.client.Configuration(ctx, &v)
	if err != nil {
		return fmt.Errorf("failed to get workspace configuration from client (%s): %v", folder, err)
	}
	for _, config := range configs {
		if err := s.handleOptionResults(ctx, source.SetOptions(o, config)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleOptionResults(ctx context.Context, results source.OptionResults) error {
	for _, result := range results {
		if result.Error != nil {
			if err := s.client.ShowMessage(ctx, &protocol.ShowMessageParams{
				Type:    protocol.Error,
				Message: result.Error.Error(),
			}); err != nil {
				return err
			}
		}
		switch result.State {
		case source.OptionUnexpected:
			if err := s.client.ShowMessage(ctx, &protocol.ShowMessageParams{
				Type:    protocol.Error,
				Message: fmt.Sprintf("unexpected gopls setting %q", result.Name),
			}); err != nil {
				return err
			}
		case source.OptionDeprecated:
			msg := fmt.Sprintf("gopls setting %q is deprecated", result.Name)
			if result.Replacement != "" {
				msg = fmt.Sprintf("%s, use %q instead", msg, result.Replacement)
			}
			if err := s.client.ShowMessage(ctx, &protocol.ShowMessageParams{
				Type:    protocol.Warning,
				Message: msg,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// beginFileRequest checks preconditions for a file-oriented request and routes
// it to a snapshot.
// We don't want to return errors for benign conditions like wrong file type,
// so callers should do if !ok { return err } rather than if err != nil.
func (s *Server) beginFileRequest(ctx context.Context, pURI protocol.DocumentURI, expectKind source.FileKind) (source.Snapshot, source.VersionedFileHandle, bool, func(), error) {
	uri := pURI.SpanURI()
	if !uri.IsFile() {
		// Not a file URI. Stop processing the request, but don't return an error.
		return nil, nil, false, func() {}, nil
	}
	view, err := s.session.ViewOf(uri)
	if err != nil {
		return nil, nil, false, func() {}, err
	}
	snapshot, release := view.Snapshot(ctx)
	fh, err := snapshot.GetFile(ctx, uri)
	if err != nil {
		release()
		return nil, nil, false, func() {}, err
	}
	if expectKind != source.UnknownKind && fh.Kind() != expectKind {
		// Wrong kind of file. Nothing to do.
		release()
		return nil, nil, false, func() {}, nil
	}
	return snapshot, fh, true, release, nil
}

func (s *Server) shutdown(ctx context.Context) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.state < serverInitialized {
		event.Log(ctx, "server shutdown without initialization")
	}
	if s.state != serverShutDown {
		// drop all the active views
		s.session.Shutdown(ctx)
		s.state = serverShutDown
	}
	return nil
}

func (s *Server) exit(ctx context.Context) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	// TODO: We need a better way to find the conn close method.
	s.client.(io.Closer).Close()

	if s.state != serverShutDown {
		// TODO: We should be able to do better than this.
		os.Exit(1)
	}
	// we don't terminate the process on a normal exit, we just allow it to
	// close naturally if needed after the connection is closed.
	return nil
}
