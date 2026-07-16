//go:build windows

package host

import (
	"strings"
	"testing"
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
	host, _ := newTestHost(t, Config{
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
	// An application method must NOT reach Config.Bridge from a restricted source.
	reply = host.handleWebMessage(`{"id":"2","method":"GetSecret","args":[]}`, false)
	if called {
		t.Fatal("a restricted source reached Config.Bridge")
	}
	if !strings.Contains(reply, `"ok":false`) {
		t.Fatalf("restricted application call should be rejected, got %q", reply)
	}
	// The same method DOES reach the bridge from a trusted source (allowBridge=true).
	host.handleWebMessage(`{"id":"3","method":"GetSecret","args":[]}`, true)
	if !called {
		t.Fatal("a trusted source did not reach Config.Bridge")
	}
}
