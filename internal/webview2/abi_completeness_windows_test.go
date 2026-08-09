//go:build windows

package webview2

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestCOMABIInventoryCompleteness is an alarm, not ABI proof. It forces every
// new production vtable, COM object representation, and GUID literal into an
// explicit classification. The layout, numerical dispatch, and canonical IID
// tests remain responsible for proving the entries themselves.
// A new manifest row is not enough: add its independent literal layout/slot/IID
// proof beside interfaces_windows_test.go, handlers_windows_test.go, or the
// loader options/completion tests before extending the inventory below.
func TestCOMABIInventoryCompleteness(t *testing.T) {
	gotVtables, gotObjects, gotGUIDs := parseProductionABIInventory(t, ".")

	wantVtables := map[string]string{
		"IUnknownVtbl":                                   "shared-base",
		"eventHandlerVtbl":                               "go-implemented-callback",
		"completionVtbl":                                 "go-implemented-callback",
		"environmentOptionsVtbl":                         "go-implemented-callback",
		"ICoreWebView2ControllerVtbl":                    "runtime-owned-interface",
		"ICoreWebView2Controller2Vtbl":                   "runtime-owned-interface",
		"ICoreWebView2Controller3Vtbl":                   "runtime-owned-interface",
		"ICoreWebView2Vtbl":                              "runtime-owned-interface",
		"ISequentialStreamVtbl":                          "runtime-owned-interface",
		"IStreamVtbl":                                    "runtime-owned-interface",
		"ICoreWebView2EnvironmentVtbl":                   "runtime-owned-interface",
		"ICoreWebView2WebMessageReceivedEventArgsVtbl":   "runtime-owned-interface",
		"ICoreWebView2NavigationStartingEventArgsVtbl":   "runtime-owned-interface",
		"ICoreWebView2NavigationCompletedEventArgsVtbl":  "runtime-owned-interface",
		"ICoreWebView2ProcessFailedEventArgsVtbl":        "runtime-owned-interface",
		"ICoreWebView2NewWindowRequestedEventArgsVtbl":   "runtime-owned-interface",
		"ICoreWebView2SettingsVtbl":                      "runtime-owned-interface",
		"ICoreWebView2Settings2Vtbl":                     "runtime-owned-interface",
		"ICoreWebView2Settings3Vtbl":                     "runtime-owned-interface",
		"ICoreWebView2Settings4Vtbl":                     "runtime-owned-interface",
		"ICoreWebView2Settings5Vtbl":                     "runtime-owned-interface",
		"ICoreWebView2Settings6Vtbl":                     "runtime-owned-interface",
		"ICoreWebView2Settings7Vtbl":                     "runtime-owned-interface",
		"ICoreWebView2Settings8Vtbl":                     "runtime-owned-interface",
		"ICoreWebView2Settings9Vtbl":                     "runtime-owned-interface",
		"ICoreWebView2WebResourceRequestVtbl":            "runtime-owned-interface",
		"ICoreWebView2WebResourceRequestedEventArgsVtbl": "runtime-owned-interface",
		"ICoreWebView2WebResourceResponseVtbl":           "runtime-owned-interface",
	}
	wantObjects := map[string]string{
		"comServer":                                  "go-implementation-base",
		"eventHandler":                               "go-implemented-object",
		"completedHandler":                           "go-implemented-object",
		"environmentOptions":                         "go-implemented-object",
		"IUnknown":                                   "runtime-owned-interface",
		"ICoreWebView2Controller":                    "runtime-owned-interface",
		"ICoreWebView2Controller2":                   "runtime-owned-interface",
		"ICoreWebView2Controller3":                   "runtime-owned-interface",
		"ICoreWebView2":                              "runtime-owned-interface",
		"IStream":                                    "runtime-owned-interface",
		"ICoreWebView2Environment":                   "runtime-owned-interface",
		"ICoreWebView2WebMessageReceivedEventArgs":   "runtime-owned-interface",
		"ICoreWebView2NavigationStartingEventArgs":   "runtime-owned-interface",
		"ICoreWebView2NavigationCompletedEventArgs":  "runtime-owned-interface",
		"ICoreWebView2ProcessFailedEventArgs":        "runtime-owned-interface",
		"ICoreWebView2NewWindowRequestedEventArgs":   "runtime-owned-interface",
		"ICoreWebView2Settings":                      "runtime-owned-interface",
		"ICoreWebView2Settings3":                     "runtime-owned-interface",
		"ICoreWebView2Settings5":                     "runtime-owned-interface",
		"ICoreWebView2Settings9":                     "runtime-owned-interface",
		"ICoreWebView2WebResourceRequest":            "runtime-owned-interface",
		"ICoreWebView2WebResourceRequestedEventArgs": "runtime-owned-interface",
		"ICoreWebView2WebResourceResponse":           "runtime-owned-interface",
	}
	wantGUIDs := map[string]string{
		"IIDIUnknown":                                      "shared-base",
		"IIDICoreWebView2Controller2":                      "queried-runtime-interface",
		"IIDICoreWebView2Controller3":                      "queried-runtime-interface",
		"IIDICoreWebView2Settings3":                        "queried-runtime-interface",
		"IIDICoreWebView2Settings5":                        "queried-runtime-interface",
		"IIDICoreWebView2Settings9":                        "queried-runtime-interface",
		"IIDICoreWebView2WebMessageReceivedEventHandler":   "go-implemented-event",
		"IIDICoreWebView2WebResourceRequestedEventHandler": "go-implemented-event",
		"IIDICoreWebView2NavigationStartingEventHandler":   "go-implemented-event",
		"IIDICoreWebView2NavigationCompletedEventHandler":  "go-implemented-event",
		"IIDICoreWebView2ProcessFailedEventHandler":        "go-implemented-event",
		"IIDICoreWebView2NewWindowRequestedEventHandler":   "go-implemented-event",
		"iidEnvironmentOptions":                            "go-implemented-loader",
		"iidEnvironmentCompletedHandler":                   "go-implemented-loader",
		"iidControllerCompletedHandler":                    "go-implemented-loader",
	}

	checkABIManifest(t, "vtable declarations", gotVtables, wantVtables)
	checkABIManifest(t, "COM object structs", gotObjects, wantObjects)
	checkABIManifest(t, "GUID literals", gotGUIDs, wantGUIDs)
}

func TestCOMABIInventoryIncludesArchitectureSpecificProductionFiles(t *testing.T) {
	dir := t.TempDir()
	source := []byte("package webview2\n\ntype probeVtbl struct { Slot uintptr }\n")
	if err := os.WriteFile(dir+"/probe_windows_amd64.go", source, 0o600); err != nil {
		t.Fatal(err)
	}

	vtables, _, _ := parseProductionABIInventory(t, dir)
	if got := vtables["probeVtbl"]; got != "go-implemented-callback" {
		t.Fatalf("architecture-specific vtable classification = %q, want go-implemented-callback", got)
	}
}

func parseProductionABIInventory(t *testing.T, dir string) (map[string]string, map[string]string, map[string]string) {
	t.Helper()
	fileset := token.NewFileSet()
	// Scan every production Go filename, not only *_windows.go: Go permits
	// architecture suffixes such as *_windows_amd64.go, and an ABI declaration
	// hidden there is exactly the kind of unmanifested platform seam this alarm
	// must make noisy.
	packages, err := parser.ParseDir(fileset, dir, func(info os.FileInfo) bool {
		name := info.Name()
		return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse production Windows files: %v", err)
	}
	pkg := packages["webview2"]
	if pkg == nil {
		t.Fatal("production webview2 package was not parsed")
	}

	vtables := make(map[string]string)
	objects := make(map[string]string)
	guids := make(map[string]string)
	for _, file := range pkg.Files {
		namedGUIDs := make(map[token.Pos]string)
		for _, declaration := range file.Decls {
			generic, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, specification := range generic.Specs {
				switch spec := specification.(type) {
				case *ast.TypeSpec:
					structure, ok := spec.Type.(*ast.StructType)
					if !ok {
						continue
					}
					name := spec.Name.Name
					if strings.HasSuffix(name, "Vtbl") {
						if name == "IUnknownVtbl" {
							vtables[name] = "shared-base"
						} else if ast.IsExported(name) {
							vtables[name] = "runtime-owned-interface"
						} else {
							vtables[name] = "go-implemented-callback"
						}
					}
					if name == "comServer" {
						objects[name] = "go-implementation-base"
					} else if structHasFieldType(structure, "comServer") {
						objects[name] = "go-implemented-object"
					} else if structHasVtablePointer(structure) {
						objects[name] = "runtime-owned-interface"
					}
				case *ast.ValueSpec:
					if len(spec.Names) != len(spec.Values) {
						continue
					}
					for index, value := range spec.Values {
						if isGUIDLiteral(value) {
							namedGUIDs[value.Pos()] = spec.Names[index].Name
						}
					}
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			expression, ok := node.(ast.Expr)
			if !ok || !isGUIDLiteral(expression) {
				return true
			}
			name := namedGUIDs[expression.Pos()]
			if name == "" {
				name = "anonymous@" + fileset.Position(expression.Pos()).String()
			}
			guids[name] = classifyGUID(name)
			return true
		})
	}
	return vtables, objects, guids
}

func structHasFieldType(structure *ast.StructType, typeName string) bool {
	for _, field := range structure.Fields.List {
		identifier, ok := field.Type.(*ast.Ident)
		if ok && identifier.Name == typeName {
			return true
		}
	}
	return false
}

func structHasVtablePointer(structure *ast.StructType) bool {
	for _, field := range structure.Fields.List {
		if len(field.Names) != 1 || field.Names[0].Name != "Vtbl" {
			continue
		}
		pointer, ok := field.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		identifier, ok := pointer.X.(*ast.Ident)
		if ok && strings.HasSuffix(identifier.Name, "Vtbl") {
			return true
		}
	}
	return false
}

func isGUIDLiteral(expression ast.Expr) bool {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return false
	}
	selector, ok := literal.Type.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "GUID"
}

func classifyGUID(name string) string {
	switch {
	case name == "IIDIUnknown":
		return "shared-base"
	case strings.HasPrefix(name, "iid"):
		return "go-implemented-loader"
	case strings.HasSuffix(name, "EventHandler"):
		return "go-implemented-event"
	default:
		return "queried-runtime-interface"
	}
}

func checkABIManifest(t *testing.T, kind string, got, want map[string]string) {
	t.Helper()
	var problems []string
	for name, classification := range got {
		wantClassification, exists := want[name]
		if !exists {
			problems = append(problems, "unclassified "+name+" (detected as "+classification+")")
		} else if classification != wantClassification {
			problems = append(problems, name+" classified as "+classification+", manifest says "+wantClassification)
		}
	}
	for name := range want {
		if _, exists := got[name]; !exists {
			problems = append(problems, "manifest entry "+name+" has no production declaration")
		}
	}
	if len(problems) == 0 {
		return
	}
	sort.Strings(problems)
	t.Errorf("%s inventory changed; classify the declaration explicitly:\n  %s", kind, strings.Join(problems, "\n  "))
}
