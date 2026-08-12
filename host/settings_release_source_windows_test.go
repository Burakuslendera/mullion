//go:build windows

package host

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestSettingsAcquisitionDefersBaseRelease keeps both production Settings()
// ownership handoffs from silently losing their base-reference release (issue
// #42). Each call site needs its own guard because the helper methods they call
// cannot release a reference they were never given.
func TestSettingsAcquisitionDefersBaseRelease(t *testing.T) {
	for _, test := range []struct {
		file     string
		function string
	}{
		{"webview_security_windows.go", "applyWebViewHardening"},
		{"webview_tab_strip_windows.go", "applyTabStripStartup"},
	} {
		t.Run(test.function, func(t *testing.T) {
			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, test.file, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			var function *ast.FuncDecl
			for _, declaration := range file.Decls {
				candidate, ok := declaration.(*ast.FuncDecl)
				if ok && candidate.Name.Name == test.function {
					function = candidate
					break
				}
			}
			if function == nil {
				t.Fatalf("production function %s not found", test.function)
			}
			var releases int
			ast.Inspect(function.Body, func(node ast.Node) bool {
				deferStatement, ok := node.(*ast.DeferStmt)
				if !ok {
					return true
				}
				selector, ok := deferStatement.Call.Fun.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "Release" {
					if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "settings" {
						releases++
					}
				}
				return true
			})
			if releases != 1 {
				t.Fatalf("%s defers settings.Release %d times, want 1", test.function, releases)
			}
		})
	}
}
