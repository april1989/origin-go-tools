// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"context"
	"fmt"
	"go/ast"
	"io/ioutil"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
	"github.com/april1989/origin-go-tools/internal/event"
	"github.com/april1989/origin-go-tools/internal/lsp/debug/tag"
	"github.com/april1989/origin-go-tools/internal/lsp/diff"
	"github.com/april1989/origin-go-tools/internal/lsp/protocol"
	"github.com/april1989/origin-go-tools/internal/lsp/source"
	"github.com/april1989/origin-go-tools/internal/memoize"
	"github.com/april1989/origin-go-tools/internal/span"
)

type modTidyKey struct {
	sessionID       string
	cfg             string
	gomod           source.FileIdentity
	imports         string
	unsavedOverlays string
	view            string
}

type modTidyHandle struct {
	handle *memoize.Handle
}

type modTidyData struct {
	tidied *source.TidiedModule
	err    error
}

func (mth *modTidyHandle) tidy(ctx context.Context, snapshot *snapshot) (*source.TidiedModule, error) {
	v, err := mth.handle.Get(ctx, snapshot.generation, snapshot)
	if err != nil {
		return nil, err
	}
	data := v.(*modTidyData)
	return data.tidied, data.err
}

func (s *snapshot) ModTidy(ctx context.Context, fh source.FileHandle) (*source.TidiedModule, error) {
	if !s.view.tmpMod {
		return nil, source.ErrTmpModfileUnsupported
	}
	if handle := s.getModTidyHandle(fh.URI()); handle != nil {
		return handle.tidy(ctx, s)
	}
	workspacePkgs, err := s.WorkspacePackages(ctx)
	if err != nil {
		return nil, err
	}
	importHash, err := hashImports(ctx, workspacePkgs)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	overlayHash := hashUnsavedOverlays(s.files)
	s.mu.Unlock()

	// Make sure to use the module root in the configuration.
	cfg := s.configWithDir(ctx, filepath.Dir(fh.URI().Filename()))
	key := modTidyKey{
		sessionID:       s.view.session.id,
		view:            s.view.root.Filename(),
		imports:         importHash,
		unsavedOverlays: overlayHash,
		gomod:           fh.FileIdentity(),
		cfg:             hashConfig(cfg),
	}
	h := s.generation.Bind(key, func(ctx context.Context, arg memoize.Arg) interface{} {
		ctx, done := event.Start(ctx, "cache.ModTidyHandle", tag.URI.Of(fh.URI()))
		defer done()

		snapshot := arg.(*snapshot)
		pm, err := snapshot.ParseMod(ctx, fh)
		if err != nil || len(pm.ParseErrors) > 0 {
			if err == nil {
				err = fmt.Errorf("could not parse module to tidy: %v", pm.ParseErrors)
			}
			var errors []source.Error
			if pm != nil {
				errors = pm.ParseErrors
			}
			return &modTidyData{
				tidied: &source.TidiedModule{
					Parsed: pm,
					Errors: errors,
				},
				err: err,
			}
		}
		tmpURI, runner, inv, cleanup, err := snapshot.goCommandInvocation(ctx, true, "mod", []string{"tidy"})
		if err != nil {
			return &modTidyData{err: err}
		}
		// Keep the temporary go.mod file around long enough to parse it.
		defer cleanup()

		if _, err := runner.Run(ctx, *inv); err != nil {
			return &modTidyData{err: err}
		}
		// Go directly to disk to get the temporary mod file, since it is
		// always on disk.
		tempContents, err := ioutil.ReadFile(tmpURI.Filename())
		if err != nil {
			return &modTidyData{err: err}
		}
		ideal, err := modfile.Parse(tmpURI.Filename(), tempContents, nil)
		if err != nil {
			// We do not need to worry about the temporary file's parse errors
			// since it has been "tidied".
			return &modTidyData{err: err}
		}
		// Compare the original and tidied go.mod files to compute errors and
		// suggested fixes.
		errors, err := modTidyErrors(ctx, snapshot, pm, ideal, workspacePkgs)
		if err != nil {
			return &modTidyData{err: err}
		}
		return &modTidyData{
			tidied: &source.TidiedModule{
				Errors:        errors,
				Parsed:        pm,
				TidiedContent: tempContents,
			},
		}
	})

	mth := &modTidyHandle{handle: h}
	s.mu.Lock()
	s.modTidyHandles[fh.URI()] = mth
	s.mu.Unlock()

	return mth.tidy(ctx, s)
}

func hashImports(ctx context.Context, wsPackages []source.Package) (string, error) {
	results := make(map[string]bool)
	var imports []string
	for _, pkg := range wsPackages {
		for _, path := range pkg.Imports() {
			imp := path.PkgPath()
			if _, ok := results[imp]; !ok {
				results[imp] = true
				imports = append(imports, imp)
			}
		}
	}
	sort.Strings(imports)
	hashed := strings.Join(imports, ",")
	return hashContents([]byte(hashed)), nil
}

// modTidyErrors computes the differences between the original and tidied
// go.mod files to produce diagnostic and suggested fixes. Some diagnostics
// may appear on the Go files that import packages from missing modules.
func modTidyErrors(ctx context.Context, snapshot source.Snapshot, pm *source.ParsedModule, ideal *modfile.File, workspacePkgs []source.Package) (errors []source.Error, err error) {
	// First, determine which modules are unused and which are missing from the
	// original go.mod file.
	var (
		unused          = make(map[string]*modfile.Require, len(pm.File.Require))
		missing         = make(map[string]*modfile.Require, len(ideal.Require))
		wrongDirectness = make(map[string]*modfile.Require, len(pm.File.Require))
	)
	for _, req := range pm.File.Require {
		unused[req.Mod.Path] = req
	}
	for _, req := range ideal.Require {
		origReq := unused[req.Mod.Path]
		if origReq == nil {
			missing[req.Mod.Path] = req
			continue
		} else if origReq.Indirect != req.Indirect {
			wrongDirectness[req.Mod.Path] = origReq
		}
		delete(unused, req.Mod.Path)
	}
	for _, req := range unused {
		srcErr, err := unusedError(pm.Mapper, req, snapshot.View().Options().ComputeEdits)
		if err != nil {
			return nil, err
		}
		errors = append(errors, srcErr)
	}
	for _, req := range wrongDirectness {
		// Handle dependencies that are incorrectly labeled indirect and
		// vice versa.
		srcErr, err := directnessError(pm.Mapper, req, snapshot.View().Options().ComputeEdits)
		if err != nil {
			return nil, err
		}
		errors = append(errors, srcErr)
	}
	// Next, compute any diagnostics for modules that are missing from the
	// go.mod file. The fixes will be for the go.mod file, but the
	// diagnostics should also appear in both the go.mod file and the import
	// statements in the Go files in which the dependencies are used.
	missingModuleFixes := map[*modfile.Require][]source.SuggestedFix{}
	for _, req := range missing {
		srcErr, err := missingModuleError(snapshot, pm, req)
		if err != nil {
			return nil, err
		}
		missingModuleFixes[req] = srcErr.SuggestedFixes
		errors = append(errors, srcErr)
	}
	// Add diagnostics for missing modules anywhere they are imported in the
	// workspace.
	for _, pkg := range workspacePkgs {
		missingImports := map[string]*modfile.Require{}
		for _, imp := range pkg.Imports() {
			if req, ok := missing[imp.PkgPath()]; ok {
				missingImports[imp.PkgPath()] = req
				break
			}
			// If the import is a package of the dependency, then add the
			// package to the map, this will eliminate the need to do this
			// prefix package search on each import for each file.
			// Example:
			//
			// import (
			//   "github.com/april1989/origin-go-tools/go/expect"
			//   "github.com/april1989/origin-go-tools/go/packages"
			// )
			// They both are related to the same module: "github.com/april1989/origin-go-tools".
			var match string
			for _, req := range ideal.Require {
				if strings.HasPrefix(imp.PkgPath(), req.Mod.Path) && len(req.Mod.Path) > len(match) {
					match = req.Mod.Path
				}
			}
			if req, ok := missing[match]; ok {
				missingImports[imp.PkgPath()] = req
			}
		}
		// None of this package's imports are from missing modules.
		if len(missingImports) == 0 {
			continue
		}
		for _, pgf := range pkg.CompiledGoFiles() {
			file, m := pgf.File, pgf.Mapper
			if file == nil || m == nil {
				continue
			}
			imports := make(map[string]*ast.ImportSpec)
			for _, imp := range file.Imports {
				if imp.Path == nil {
					continue
				}
				if target, err := strconv.Unquote(imp.Path.Value); err == nil {
					imports[target] = imp
				}
			}
			if len(imports) == 0 {
				continue
			}
			for importPath, req := range missingImports {
				imp, ok := imports[importPath]
				if !ok {
					continue
				}
				fixes, ok := missingModuleFixes[req]
				if !ok {
					return nil, fmt.Errorf("no missing module fix for %q (%q)", importPath, req.Mod.Path)
				}
				srcErr, err := missingModuleForImport(snapshot, m, imp, req, fixes)
				if err != nil {
					return nil, err
				}
				errors = append(errors, srcErr)
			}
		}
	}
	return errors, nil
}

// unusedError returns a source.Error for an unused require.
func unusedError(m *protocol.ColumnMapper, req *modfile.Require, computeEdits diff.ComputeEdits) (source.Error, error) {
	rng, err := rangeFromPositions(m, req.Syntax.Start, req.Syntax.End)
	if err != nil {
		return source.Error{}, err
	}
	edits, err := dropDependency(req, m, computeEdits)
	if err != nil {
		return source.Error{}, err
	}
	return source.Error{
		Category: source.GoModTidy,
		Message:  fmt.Sprintf("%s is not used in this module", req.Mod.Path),
		Range:    rng,
		URI:      m.URI,
		SuggestedFixes: []source.SuggestedFix{{
			Title: fmt.Sprintf("Remove dependency: %s", req.Mod.Path),
			Edits: map[span.URI][]protocol.TextEdit{
				m.URI: edits,
			},
		}},
	}, nil
}

// directnessError extracts errors when a dependency is labeled indirect when
// it should be direct and vice versa.
func directnessError(m *protocol.ColumnMapper, req *modfile.Require, computeEdits diff.ComputeEdits) (source.Error, error) {
	rng, err := rangeFromPositions(m, req.Syntax.Start, req.Syntax.End)
	if err != nil {
		return source.Error{}, err
	}
	direction := "indirect"
	if req.Indirect {
		direction = "direct"

		// If the dependency should be direct, just highlight the // indirect.
		if comments := req.Syntax.Comment(); comments != nil && len(comments.Suffix) > 0 {
			end := comments.Suffix[0].Start
			end.LineRune += len(comments.Suffix[0].Token)
			end.Byte += len([]byte(comments.Suffix[0].Token))
			rng, err = rangeFromPositions(m, comments.Suffix[0].Start, end)
			if err != nil {
				return source.Error{}, err
			}
		}
	}
	// If the dependency should be indirect, add the // indirect.
	edits, err := switchDirectness(req, m, computeEdits)
	if err != nil {
		return source.Error{}, err
	}
	return source.Error{
		Message:  fmt.Sprintf("%s should be %s", req.Mod.Path, direction),
		Range:    rng,
		URI:      m.URI,
		Category: source.GoModTidy,
		SuggestedFixes: []source.SuggestedFix{{
			Title: fmt.Sprintf("Change %s to %s", req.Mod.Path, direction),
			Edits: map[span.URI][]protocol.TextEdit{
				m.URI: edits,
			},
		}},
	}, nil
}

func missingModuleError(snapshot source.Snapshot, pm *source.ParsedModule, req *modfile.Require) (source.Error, error) {
	start, end := pm.File.Module.Syntax.Span()
	rng, err := rangeFromPositions(pm.Mapper, start, end)
	if err != nil {
		return source.Error{}, err
	}
	edits, err := addRequireFix(pm.Mapper, req, snapshot.View().Options().ComputeEdits)
	if err != nil {
		return source.Error{}, err
	}
	fix := &source.SuggestedFix{
		Title: fmt.Sprintf("Add %s to your go.mod file", req.Mod.Path),
		Edits: map[span.URI][]protocol.TextEdit{
			pm.Mapper.URI: edits,
		},
	}
	return source.Error{
		URI:            pm.Mapper.URI,
		Range:          rng,
		Message:        fmt.Sprintf("%s is not in your go.mod file", req.Mod.Path),
		Category:       source.GoModTidy,
		Kind:           source.ModTidyError,
		SuggestedFixes: []source.SuggestedFix{*fix},
	}, nil
}

// dropDependency returns the edits to remove the given require from the go.mod
// file.
func dropDependency(req *modfile.Require, m *protocol.ColumnMapper, computeEdits diff.ComputeEdits) ([]protocol.TextEdit, error) {
	// We need a private copy of the parsed go.mod file, since we're going to
	// modify it.
	copied, err := modfile.Parse("", m.Content, nil)
	if err != nil {
		return nil, err
	}
	if err := copied.DropRequire(req.Mod.Path); err != nil {
		return nil, err
	}
	copied.Cleanup()
	newContent, err := copied.Format()
	if err != nil {
		return nil, err
	}
	// Calculate the edits to be made due to the change.
	diff := computeEdits(m.URI, string(m.Content), string(newContent))
	return source.ToProtocolEdits(m, diff)
}

// switchDirectness gets the edits needed to change an indirect dependency to
// direct and vice versa.
func switchDirectness(req *modfile.Require, m *protocol.ColumnMapper, computeEdits diff.ComputeEdits) ([]protocol.TextEdit, error) {
	// We need a private copy of the parsed go.mod file, since we're going to
	// modify it.
	copied, err := modfile.Parse("", m.Content, nil)
	if err != nil {
		return nil, err
	}
	// Change the directness in the matching require statement. To avoid
	// reordering the require statements, rewrite all of them.
	var requires []*modfile.Require
	for _, r := range copied.Require {
		if r.Mod.Path == req.Mod.Path {
			requires = append(requires, &modfile.Require{
				Mod:      r.Mod,
				Syntax:   r.Syntax,
				Indirect: !r.Indirect,
			})
			continue
		}
		requires = append(requires, r)
	}
	copied.SetRequire(requires)
	newContent, err := copied.Format()
	if err != nil {
		return nil, err
	}
	// Calculate the edits to be made due to the change.
	diff := computeEdits(m.URI, string(m.Content), string(newContent))
	return source.ToProtocolEdits(m, diff)
}

// missingModuleForImport creates an error for a given import path that comes
// from a missing module.
func missingModuleForImport(snapshot source.Snapshot, m *protocol.ColumnMapper, imp *ast.ImportSpec, req *modfile.Require, fixes []source.SuggestedFix) (source.Error, error) {
	if req.Syntax == nil {
		return source.Error{}, fmt.Errorf("no syntax for %v", req)
	}
	spn, err := span.NewRange(snapshot.FileSet(), imp.Path.Pos(), imp.Path.End()).Span()
	if err != nil {
		return source.Error{}, err
	}
	rng, err := m.Range(spn)
	if err != nil {
		return source.Error{}, err
	}
	return source.Error{
		Category:       source.GoModTidy,
		URI:            m.URI,
		Range:          rng,
		Message:        fmt.Sprintf("%s is not in your go.mod file", req.Mod.Path),
		Kind:           source.ModTidyError,
		SuggestedFixes: fixes,
	}, nil
}

// addRequireFix creates edits for adding a given require to a go.mod file.
func addRequireFix(m *protocol.ColumnMapper, req *modfile.Require, computeEdits diff.ComputeEdits) ([]protocol.TextEdit, error) {
	// We need a private copy of the parsed go.mod file, since we're going to
	// modify it.
	copied, err := modfile.Parse("", m.Content, nil)
	if err != nil {
		return nil, err
	}
	// Calculate the quick fix edits that need to be made to the go.mod file.
	if err := copied.AddRequire(req.Mod.Path, req.Mod.Version); err != nil {
		return nil, err
	}
	copied.SortBlocks()
	newContents, err := copied.Format()
	if err != nil {
		return nil, err
	}
	// Calculate the edits to be made due to the change.
	diff := computeEdits(m.URI, string(m.Content), string(newContents))
	return source.ToProtocolEdits(m, diff)
}

func rangeFromPositions(m *protocol.ColumnMapper, s, e modfile.Position) (protocol.Range, error) {
	toPoint := func(offset int) (span.Point, error) {
		l, c, err := m.Converter.ToPosition(offset)
		if err != nil {
			return span.Point{}, err
		}
		return span.NewPoint(l, c, offset), nil
	}
	start, err := toPoint(s.Byte)
	if err != nil {
		return protocol.Range{}, err
	}
	end, err := toPoint(e.Byte)
	if err != nil {
		return protocol.Range{}, err
	}
	return m.Range(span.New(m.URI, start, end))
}
