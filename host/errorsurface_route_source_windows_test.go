//go:build windows

package host

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestErrorSurfaceUsesResolvedFallbackTarget locks the production handoff from
// a classified error surface to Browser.Navigate. Testing errorPageURL alone
// would not detect a fallback redirected to about:blank (issue #3).
func TestErrorSurfaceUsesResolvedFallbackTarget(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "errorsurface_windows.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var show *ast.FuncDecl
	for _, declaration := range file.Decls {
		candidate, ok := declaration.(*ast.FuncDecl)
		if ok && candidate.Name.Name == "showErrorSurface" {
			show = candidate
			break
		}
	}
	if show == nil {
		t.Fatal("showErrorSurface not found")
	}
	var targetCalls, navigations int
	ast.Inspect(show.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch selectorPath(call.Fun) {
		case "errorPageURL":
			if len(call.Args) == 2 && selectorPath(call.Args[0]) == "host.config" && selectorPath(call.Args[1]) == "host.source.retryTarget" {
				targetCalls++
			}
		case "browser.Navigate":
			if len(call.Args) == 1 && selectorPath(call.Args[0]) == "url" {
				navigations++
			}
		}
		return true
	})
	if targetCalls != 1 || navigations != 1 {
		t.Fatalf("showErrorSurface target/Navigate calls = %d/%d, want 1/1", targetCalls, navigations)
	}
}

// TestRunDefersPostCreateTeardown keeps pre-loop failures from returning with a
// live HWND or re-posted quit message (issue #48).
func TestRunDefersPostCreateTeardown(t *testing.T) {
	source, err := os.ReadFile("host_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(source), `defer host.destroyWindowOutsideLoop("run_exit")`); got != 1 {
		t.Fatalf("Run post-create teardown defers = %d, want 1", got)
	}
}

// TestMessageLoopExitDestroysLiveWindow keeps GetMessage failure and bare
// WM_QUIT from returning while a live HWND still belongs to the Host (issue #54).
func TestMessageLoopExitDestroysLiveWindow(t *testing.T) {
	source, err := os.ReadFile("window_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(source), `host.destroyWindowOutsideLoop("message_loop_exit")`); got != 1 {
		t.Fatalf("message-loop exit teardown calls = %d, want 1", got)
	}
}

// TestCreateWindowUsesResolvedInitialPlacement keeps the tested centering
// decision on the production CreateWindowEx argument path (issue #59).
func TestCreateWindowUsesResolvedInitialPlacement(t *testing.T) {
	source, err := os.ReadFile("window_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"if place, ok := host.initialWindowPlacement(); ok {",
		"x, y = uintptr(place.X), uintptr(place.Y)",
		"width, height = place.Width, place.Height",
	} {
		if !strings.Contains(string(source), want) {
			t.Fatalf("createWindow no longer uses initial placement: missing %q", want)
		}
	}
}

// TestDeferredBoundsCallersKeepActionSource verifies each producer passes its
// distinct source code into the deferred bounds protocol (issue #46).
func TestDeferredBoundsCallersKeepActionSource(t *testing.T) {
	tests := []struct {
		file     string
		function string
		want     string
	}{
		{"control_windows.go", "toggleMaximiseFromMessage", "boundsSyncWParamDeferredRestore"},
		{"control_windows.go", "toggleMaximiseFromMessage", "boundsSyncWParamDeferredMaximize"},
		{"windowproc_windows.go", "windowProc", "boundsSyncWParamDeferredExitSizeMove"},
	}
	for _, test := range tests {
		t.Run(test.file+"/"+test.want, func(t *testing.T) {
			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, test.file, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Name.Name != test.function {
					continue
				}
				ast.Inspect(function.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok || selectorPath(call.Fun) != "host.requestDeferredBoundsSync" || len(call.Args) != 1 {
						return true
					}
					if argument, ok := call.Args[0].(*ast.Ident); ok && argument.Name == test.want {
						found = true
					}
					return true
				})
			}
			if !found {
				t.Fatalf("%s does not post deferred bounds source %q", test.function, test.want)
			}
		})
	}
}

// TestRasterizationScaleReachesEmbedAndDPIChange locks the production paths
// behind the pure DPI-to-scale mapping: a WebView opened or moved at a new DPI
// must receive the matching rasterization scale (issue #1).
func TestRasterizationScaleReachesEmbedAndDPIChange(t *testing.T) {
	tests := []struct {
		file string
		want string
	}{
		{"webview_windows.go", `host.syncRasterizationScale("embed", dpiForWindow(host.window()))`},
		{"windowproc_windows.go", `host.syncRasterizationScale("wm_dpi_changed", uint32(wParam&0xffff))`},
	}
	for _, test := range tests {
		source, err := os.ReadFile(test.file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(source), test.want) {
			t.Fatalf("%s missing rasterization scale call %q", test.file, test.want)
		}
	}
}

func selectorPath(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.SelectorExpr:
		prefix := selectorPath(expression.X)
		if prefix == "" {
			return ""
		}
		return prefix + "." + expression.Sel.Name
	default:
		return ""
	}
}
