// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package paneltest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/sdk/nebla"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const internalDir = "../../../internal"

// dynamicCodes are codes the panel builds at run time, so no literal reaches the walk.
var dynamicCodes = []string{}

// codeCall says which argument of a call carries the error code.
type codeCall struct {
	pkg, fn string
	arg     int
}

var codeCalls = []codeCall{
	{"apierr", "New", 1},
	{"", "WriteError", 2},
}

func parseInternal(t *testing.T) map[string]*ast.File {
	t.Helper()
	files := map[string]*ast.File{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(internalDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		files[path] = f
		return nil
	})
	require.NoError(t, err)
	return files
}

func literal(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	return s, err == nil
}

func callName(c *ast.CallExpr) (pkg, fn string) {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return "", f.Name
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok {
			return id.Name, f.Sel.Name
		}
		return "", f.Sel.Name
	}
	return "", ""
}

// forwardingHelpers finds functions that pass one of their string parameters on as an apierr.New code.
func forwardingHelpers(files map[string]*ast.File) map[string]int {
	helpers := map[string]int{}
	for _, f := range files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			params := map[string]int{}
			i := 0
			for _, field := range fd.Type.Params.List {
				for _, n := range field.Names {
					params[n.Name] = i
					i++
				}
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if pkg, fn := callName(c); pkg == "apierr" && fn == "New" && len(c.Args) > 1 {
					if id, ok := c.Args[1].(*ast.Ident); ok {
						if idx, isParam := params[id.Name]; isParam {
							helpers[fd.Name.Name] = idx
						}
					}
				}
				return true
			})
		}
	}
	return helpers
}

// actionCodes reads the string constants named Code* in internal/actions.
func actionCodes(files map[string]*ast.File) []string {
	var out []string
	for path, f := range files {
		if filepath.Base(filepath.Dir(path)) != "actions" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Code") || i >= len(vs.Values) {
					continue
				}
				if s, ok := literal(vs.Values[i]); ok {
					out = append(out, s)
				}
			}
			return true
		})
	}
	return out
}

func TestErrorCodesCovered(t *testing.T) {
	files := parseInternal(t)
	helpers := forwardingHelpers(files)
	found := map[string]string{}
	for path, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			pkg, fn := callName(c)
			idx := -1
			for _, cc := range codeCalls {
				if cc.fn == fn && (cc.pkg == "" || cc.pkg == pkg) {
					idx = cc.arg
				}
			}
			if h, ok := helpers[fn]; ok && idx < 0 {
				idx = h
			}
			if idx >= 0 && idx < len(c.Args) {
				if s, ok := literal(c.Args[idx]); ok {
					found[s] = path
				}
			}
			return true
		})
	}
	for _, s := range actionCodes(files) {
		found[s] = "internal/actions"
	}
	for _, s := range dynamicCodes {
		found[s] = "allowlist"
	}
	require.NotEmpty(t, found)

	known := map[string]bool{}
	for _, c := range nebla.KnownCodes() {
		known[string(c)] = true
	}
	for code, path := range found {
		assert.True(t, known[code], "panel code %q (%s) is not a nebla.ErrorCode", code, path)
	}
	for _, c := range []string{"conflict", "locked", "not_found"} {
		assert.True(t, slices.Contains(sortedKeys(found), c), "walk did not find %q; helper detection is broken", c)
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
