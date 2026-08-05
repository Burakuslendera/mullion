//go:build windows

package host

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

// TestBridgeHandlesWindowControlsWithoutAConfiguredBridge is the whole point of
// the reserved-method router. An application that only wants a window - no
// application methods at all - leaves Config.Bridge nil, and the title bar must
// still work. Before the router existed, every consumer had to re-implement the
// window protocol or get a dead title bar.
func TestBridgeHandlesWindowControlsWithoutAConfiguredBridge(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true})

	for _, method := range []string{
		methodStartDrag, methodMinimise, methodToggleMaximise,
		methodHide, methodPhase, methodDiagnostic,
	} {
		reply := host.handleWebMessage(`{"id":"1","method":"`+method+`","args":[]}`, true)
		if !strings.Contains(reply, `"ok":true`) {
			t.Fatalf("reserved method %q was not handled by the host: %q", method, reply)
		}
	}

	reply := host.handleWebMessage(`{"id":"7","method":"`+methodIsMaximised+`","args":[]}`, true)
	if reply != `{"id":"7","ok":true,"result":false}` {
		t.Fatalf("IsMaximised reply = %q", reply)
	}
}

// TestBridgeForwardsUnknownMethodsVerbatim locks the other half of the contract:
// the application's own wire format stays opaque to the library. The router must
// hand over the original string, not a re-encoded one.
func TestBridgeForwardsUnknownMethodsVerbatim(t *testing.T) {
	const raw = `{"id":"9","method":"GetThings","args":[{"page":2}]}`

	var seen string
	host, _ := newTestHost(t, Config{
		StartHidden: true,
		Bridge: func(message string) string {
			seen = message
			return `{"id":"9","ok":true,"result":["a"]}`
		},
	})

	reply := host.handleWebMessage(raw, true)
	if seen != raw {
		t.Fatalf("Bridge received a rewritten message:\n got %q\nwant %q", seen, raw)
	}
	if reply != `{"id":"9","ok":true,"result":["a"]}` {
		t.Fatalf("reply = %q", reply)
	}
}

func TestBridgeBoundsDiagnosticsButForwardsLargeRawRequestVerbatim(t *testing.T) {
	method := "Call" + strings.Repeat(".:;,", logsafe.DiagnosticLimit*2)
	raw := `{"id":"large","method":` + strconv.Quote(method) + `,"args":[{"opaque":"unchanged"}]}`
	var seen string
	host, logger := newTestHost(t, Config{
		StartHidden: true,
		Bridge: func(message string) string {
			seen = message
			return ""
		},
	})

	host.handleWebMessage(raw, true)

	if seen != raw {
		t.Fatalf("Bridge received a rewritten request: got len %d, want len %d", len(seen), len(raw))
	}
	host.diagnostics.mu.Lock()
	lastBridge := host.diagnostics.lastBridge
	host.diagnostics.mu.Unlock()
	if len(lastBridge) > logsafe.DiagnosticLimit {
		t.Fatalf("retained lastBridge len = %d, limit = %d", len(lastBridge), logsafe.DiagnosticLimit)
	}
	if strings.Contains(logger.String(), method) {
		t.Fatal("logger received the unbounded frontend method")
	}
}

func TestBridgeUnknownMethodWithoutBridgeIsAnError(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true})

	reply := host.handleWebMessage(`{"id":"3","method":"GetThings","args":[]}`, true)
	if !strings.Contains(reply, `"ok":false`) {
		t.Fatalf("unknown method without a bridge should fail, got %q", reply)
	}
}

// TestBridgeSurvivesMalformedInput: chrome.webview.postMessage accepts arbitrary
// strings, so a frontend bug must not be able to take the window down.
func TestBridgeSurvivesMalformedInput(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})

	for _, raw := range []string{"", "not json", "[]", "{}", `{"id":"1"}`, `{"method":""}`} {
		if reply := host.handleWebMessage(raw, true); reply != "" {
			t.Fatalf("malformed message %q produced a reply: %q", raw, reply)
		}
	}
	if !strings.Contains(logger.String(), "mullion: bridge message") {
		t.Fatal("malformed messages were dropped without a trace")
	}
}

// TestBridgeReservedMethodsNeverReachTheApplication: a frontend that calls
// window.<ns>.window.minimise() must not be able to make the application's
// Bridge see a "WindowMinimise" method it never declared.
func TestBridgeReservedMethodsNeverReachTheApplication(t *testing.T) {
	called := false
	host, _ := newTestHost(t, Config{
		StartHidden: true,
		Bridge: func(string) string {
			called = true
			return ""
		},
	})

	host.handleWebMessage(`{"id":"1","method":"`+methodMinimise+`","args":[]}`, true)
	host.handleWebMessage(`{"id":"2","method":"`+methodDiagnostic+`","args":["phase","boot"]}`, true)

	if called {
		t.Fatal("a reserved method was forwarded to Config.Bridge")
	}
}

// TestBridgeRejectsUnknownResizeEdge: StartResize validates its own input, so a
// bad edge is logged and dropped rather than posted to the window procedure as a
// nonsense hit-test code.
func TestBridgeRejectsUnknownResizeEdge(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})

	host.handleWebMessage(`{"id":"1","method":"`+methodStartResize+`","args":["sideways"]}`, true)

	if !strings.Contains(logger.String(), "resize requested with unknown edge") {
		t.Fatalf("unknown resize edge was not rejected:\n%s", logger.String())
	}
}

// TestBridgeRestrictedSourceReachesOnlyReservedMethods locks the data:-source
// containment (decisions/0014). A restricted source - a data: document, which a
// hostile script could be posting from a data: iframe - may drive the reserved
// window controls, but a non-reserved method must never reach Config.Bridge.
func TestBridgeRestrictedSourceReachesOnlyReservedMethods(t *testing.T) {
	called := false
	host, logger := newTestHost(t, Config{
		StartHidden: true,
		Bridge: func(string) string {
			called = true
			return `{"id":"x","ok":true}`
		},
	})

	// A reserved window control still works from a restricted source.
	reply := host.handleWebMessage(`{"id":"1","method":"`+methodMinimise+`","args":[]}`, false)
	if !strings.Contains(reply, `"ok":true`) {
		t.Fatalf("reserved method blocked from a restricted source: %q", reply)
	}
	// An application method must NOT reach Config.Bridge from a restricted source,
	// and gets no reply - the same no-correlation stance the outer origin gate
	// takes for a foreign source, so a hostile data: iframe cannot confirm it
	// holds the restricted admission (decisions/0014, issue #70).
	reply = host.handleWebMessage(`{"id":"2","method":"GetSecret","args":[]}`, false)
	if called {
		t.Fatal("a restricted source reached Config.Bridge")
	}
	if reply != "" {
		t.Fatalf("restricted application call should get no reply, got %q", reply)
	}
	// The rejection is still diagnosed host-side; only the frontend reply is
	// withheld, so the drop must not become silent to the operator too.
	if !strings.Contains(logger.String(), "rejected from a restricted source") {
		t.Fatalf("restricted rejection was not logged:\n%s", logger.String())
	}
	// The same method DOES reach the bridge from a trusted source (allowBridge=true).
	host.handleWebMessage(`{"id":"3","method":"GetSecret","args":[]}`, true)
	if !called {
		t.Fatal("a trusted source did not reach Config.Bridge")
	}
}

func TestRestrictedSourceDiagnosticsUseTheSameBoundedBoundary(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})
	phase := strings.Repeat(".:;,", logsafe.DiagnosticLimit*2)
	phaseRaw := `{"id":"phase","method":"` + methodPhase + `","args":[` + strconv.Quote(phase) + `]}`
	if reply := host.handleWebMessage(phaseRaw, false); !strings.Contains(reply, `"ok":true`) {
		t.Fatalf("restricted phase was not handled: %q", reply)
	}

	host.diagnostics.mu.Lock()
	retainedPhase := host.diagnostics.lastFrontendPhase
	host.diagnostics.mu.Unlock()
	if len(retainedPhase) > logsafe.DiagnosticLimit {
		t.Fatalf("restricted retained phase len = %d, limit = %d", len(retainedPhase), logsafe.DiagnosticLimit)
	}

	detail := strings.Repeat("context ", logsafe.DiagnosticLimit) +
		"https://mullion.localhost/app/main.js?secret=value"
	diagnosticRaw := `{"id":"diagnostic","method":"` + methodDiagnostic +
		`","args":["error",` + strconv.Quote(detail) + `]}`
	if reply := host.handleWebMessage(diagnosticRaw, false); !strings.Contains(reply, `"ok":true`) {
		t.Fatalf("restricted diagnostic was not handled: %q", reply)
	}
	if !strings.Contains(logger.String(), "https://mullion.localhost/app/main.js?") {
		t.Fatalf("restricted diagnostic lost its first URL:\n%s", logger.String())
	}
}
