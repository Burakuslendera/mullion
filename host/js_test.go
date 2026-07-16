package host

import (
	"strings"
	"testing"
)

// TestJSScriptsRenderNamespace locks the frontend contract. Everything the page
// touches - the global, the bridge state, the DOM attributes - is derived from
// Config.JSNamespace, so a host configured for "acme" must leave no "mullion"
// behind for a second host to collide with.
func TestJSScriptsRenderNamespace(t *testing.T) {
	scripts := Config{JSNamespace: "acme"}.normalise().jsScripts()

	// The default namespace must not survive anywhere it would be addressable
	// from the page. Error strings may still name the library ("mullion: bridge
	// unavailable") - that is the product name, not the namespace.
	forbidden := []string{"window.mullion", "data-mullion-", "mullionResizeEdge"}

	for name, script := range map[string]string{
		"bridge":      scripts.bridge,
		"diagnostics": scripts.diagnostics,
		"drag":        scripts.drag,
		"resize":      scripts.resize,
		"tabFlag":     scripts.tabFlag,
	} {
		if strings.Contains(script, "__NS__") {
			t.Fatalf("%s script still has an unrendered placeholder", name)
		}
		for _, needle := range forbidden {
			if strings.Contains(script, needle) {
				t.Fatalf("%s script leaked the default namespace (%q) under a custom one", name, needle)
			}
		}
	}

	if !strings.Contains(scripts.bridge, "window.acme = {") {
		t.Fatalf("bridge script does not install window.acme:\n%s", scripts.bridge)
	}
	if !strings.Contains(scripts.resize, `zone.setAttribute("data-acme-resize-edge", edge)`) {
		t.Fatal("resize overlay does not tag its zones with the namespaced attribute")
	}
	if !strings.Contains(scripts.resize, "target.dataset.acmeResizeEdge") {
		t.Fatal("resize overlay does not read back the namespaced dataset key")
	}
	if !strings.Contains(scripts.drag, `target.closest("[data-acme-drag]")`) {
		t.Fatal("drag fallback does not use the namespaced drag selector")
	}
}

// TestJSScriptsRenderGeometry proves the injected overlay is laid out from
// Config, not from constants: a 48px title bar must produce a 48px offset in the
// zone styles, or the resize band and the visible bar drift apart.
func TestJSScriptsRenderGeometry(t *testing.T) {
	scripts := Config{
		TitlebarHeight:       48,
		CaptionControlsWidth: 150,
		ResizeBorder:         6,
	}.normalise().jsScripts()

	for _, want := range []string{
		"const border = 6;",
		"const titlebarHeight = 48;",
		"const captionControlsWidth = 150;",
	} {
		if !strings.Contains(scripts.resize, want) {
			t.Fatalf("resize script missing %q", want)
		}
	}
	if !strings.Contains(scripts.drag, "const topResizeBorder = 6;") {
		t.Fatal("drag script did not pick up the resize border")
	}
}

// TestJSBridgeExposesReservedMethods keeps the JavaScript side and the Go router
// in step. If a method name is renamed on one side only, the title bar silently
// stops working; this fails the build instead.
func TestJSBridgeExposesReservedMethods(t *testing.T) {
	scripts := Config{}.normalise().jsScripts()
	for _, method := range []string{
		methodStartDrag, methodStartResize, methodMinimise, methodToggleMaximise,
		methodIsMaximised, methodShow, methodHide, methodClose,
		methodShellReady, methodReady, methodPhase, methodDiagnostic,
	} {
		if !strings.Contains(scripts.bridge, `"`+method+`"`) {
			t.Fatalf("bridge script does not call reserved method %q", method)
		}
	}
}

func TestJSStartupContextReflectsStartHidden(t *testing.T) {
	visible := Config{}.normalise().jsScripts()
	if !strings.Contains(visible.bridge, "startup: { startHidden: false }") {
		t.Fatal("startup context does not report a visible start")
	}
	hidden := Config{StartHidden: true}.normalise().jsScripts()
	if !strings.Contains(hidden.bridge, "startup: { startHidden: true }") {
		t.Fatal("startup context does not report a hidden start")
	}
}

// TestJSDragSelectorIsEscaped locks the fix for a JS-string-literal injection.
// Config.DragSelector lands inside target.closest(...) in the fallback drag
// script and, unlike JSNamespace, is not otherwise validated - so it must be
// encoded as a JS string literal rather than substituted raw.
func TestJSDragSelectorIsEscaped(t *testing.T) {
	scripts := Config{DragSelector: `x"),(globalThis.pwned=1),("`}.normalise().jsScripts()

	// Raw substitution would have produced this break-out; it must not appear.
	if strings.Contains(scripts.drag, `closest("x"),(globalThis.pwned=1),("")`) {
		t.Fatalf("drag selector broke out of its string literal:\n%s", scripts.drag)
	}
	// The value must appear only as a properly escaped JS string literal.
	if !strings.Contains(scripts.drag, `closest("x\"),(globalThis.pwned=1),(\"")`) {
		t.Fatalf("drag selector not encoded as a JS string literal:\n%s", scripts.drag)
	}
}
