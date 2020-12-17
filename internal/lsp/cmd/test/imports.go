// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmdtest

import (
	"testing"

	"github.tamu.edu/April1989/go_tools/internal/lsp/diff"
	"github.tamu.edu/April1989/go_tools/internal/lsp/diff/myers"
	"github.tamu.edu/April1989/go_tools/internal/span"
)

func (r *runner) Import(t *testing.T, spn span.Span) {
	uri := spn.URI()
	filename := uri.Filename()
	got, _ := r.NormalizeGoplsCmd(t, "imports", filename)
	want := string(r.data.Golden("goimports", filename, func() ([]byte, error) {
		return []byte(got), nil
	}))
	if want != got {
		d := myers.ComputeEdits(uri, want, got)
		t.Errorf("imports failed for %s, expected:\n%s", filename, diff.ToUnified("want", "got", want, d))
	}
}
