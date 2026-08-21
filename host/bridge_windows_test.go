//go:build windows

package host

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/logsafe"
	"github.com/Burakuslendera/mullion/internal/webview2"
)

// TestBridgeHandlesWindowControlsWithoutAConfiguredBridge checks that the
// reserved router returns successful replies for the enumerated host methods
// when Config.Bridge is nil. It creates neither a WebView nor a native window,
// so it does not prove that a real title bar renders or that its gestures work.
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
	frameReply := host.handleWebMessage(`{"id":"8","method":"`+methodFrameState+`","args":[]}`, true)
	if frameReply != `{"id":"8","ok":true,"result":{"maximised":false,"moveSizeActive":false,"generation":0}}` {
		t.Fatalf("FrameState reply = %q", frameReply)
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

type restrictedFallbackControl struct {
	method string
	args   string
	log    string
}

// Keep the restricted fallback capability set owned in one fixed-size fixture.
// The production-callback test below and the direct dispatcher contract both
// consume it, so WindowFrameState cannot silently disappear while prose still
// describes all seven controls.
var restrictedFallbackControls = [7]restrictedFallbackControl{
	{methodStartDrag, "[]", "titlebar drag requested"},
	{methodStartResize, `["left"]`, "resize requested, edge=left"},
	{methodMinimise, "[]", "minimize requested"},
	{methodToggleMaximise, "[]", "maximize toggle requested"},
	{methodIsMaximised, "[]", ""},
	{methodFrameState, "[]", ""},
	{methodClose, "[]", "quit requested"},
}

// TestBridgeRestrictedSourcePreservesWatchdogEvidenceAndWindowControls is the
// headless fallback-surface regression. The failed application owns the render
// watchdog evidence. diagnostics.js is injected into the data: fallback too, but
// its phase, diagnostic and readiness posts must not replace that evidence or
// disarm the watchdog. The fallback still needs every caption/drag/resize method
// it actually calls.
func TestBridgeRestrictedSourcePreservesWatchdogEvidenceAndWindowControls(t *testing.T) {
	called := false
	host, logger := newTestHost(t, Config{
		StartHidden: true,
		Bridge: func(string) string {
			called = true
			return `{"id":"x","ok":true}`
		},
	})
	host.MarkFrontendPhase("application bootstrap")
	before := host.diagnostics.timeoutSummary()

	rejected := []string{
		`{"id":"shell","method":"` + methodShellReady + `","args":[]}`,
		`{"id":"ready","method":"` + methodReady + `","args":[]}`,
		`{"id":"phase","method":"` + methodPhase + `","args":["fallback document created"]}`,
		`{"id":"diagnostic","method":"` + methodDiagnostic + `","args":["error","fallback diagnostic"]}`,
		`{"id":"show","method":"` + methodShow + `","args":[]}`,
		`{"id":"hide","method":"` + methodHide + `","args":[]}`,
	}
	for _, raw := range rejected {
		if reply := host.handleWebMessage(raw, false); reply != "" {
			t.Fatalf("restricted non-fallback method produced a reply: %q", reply)
		}
	}

	seenControls := make(map[string]struct{}, len(restrictedFallbackControls))
	for _, control := range restrictedFallbackControls {
		if !errorSurfaceMethodAllowed(control.method) {
			t.Fatalf("fallback control %q is absent from the restricted capability set", control.method)
		}
		if _, duplicate := seenControls[control.method]; duplicate {
			t.Fatalf("restricted fallback capability fixture repeats %q", control.method)
		}
		seenControls[control.method] = struct{}{}
	}
	if len(seenControls) != 7 {
		t.Fatalf("restricted fallback capability count = %d, want exactly 7", len(seenControls))
	}
	if _, ok := seenControls[methodFrameState]; !ok {
		t.Fatal("restricted fallback capabilities omit WindowFrameState")
	}

	for id, control := range restrictedFallbackControls {
		raw := `{"id":` + strconv.Quote(strconv.Itoa(id)) + `,"method":` +
			strconv.Quote(control.method) + `,"args":` + control.args + `}`
		reply := host.handleWebMessage(raw, false)
		if !strings.Contains(reply, `"ok":true`) {
			t.Fatalf("fallback control %q was blocked: %q", control.method, reply)
		}
		if control.log != "" && !strings.Contains(logger.String(), control.log) {
			t.Fatalf("fallback control %q did not execute; log missing %q:\n%s", control.method, control.log, logger.String())
		}
	}

	if reply := host.handleWebMessage(`{"id":"app","method":"GetSecret","args":[]}`, false); reply != "" {
		t.Fatalf("restricted application call should get no reply, got %q", reply)
	}
	if called {
		t.Fatal("a restricted source reached Config.Bridge")
	}
	host.renderMu.Lock()
	ready, shellReady := host.frontendReady, host.frontendShellReady
	host.renderMu.Unlock()
	if ready || shellReady {
		t.Fatalf("fallback changed readiness: ready=%t shellReady=%t", ready, shellReady)
	}
	if after := host.diagnostics.timeoutSummary(); after != before {
		t.Fatalf("fallback changed retained watchdog evidence:\nbefore %q\nafter  %q", before, after)
	}
	if !strings.Contains(logger.String(), "rejected from a restricted source") {
		t.Fatalf("restricted rejections were not logged:\n%s", logger.String())
	}

	// Trusted-origin reserved behavior and raw Config.Bridge admission are
	// unchanged: the same application method reaches the configured bridge.
	host.handleWebMessage(`{"id":"trusted","method":"GetSecret","args":[]}`, true)
	if !called {
		t.Fatal("a trusted source did not reach Config.Bridge")
	}
}

func TestProductionCallbacksClaimKnownEmptyFallbackBeforePinAndRestrictItsBridge(t *testing.T) {
	var bridgeCalls int
	host, logger := newTestHost(t, Config{
		StartHidden:           true,
		PinNavigationToOrigin: true,
		Bridge: func(string) string {
			bridgeCalls++
			return ""
		},
	})
	host.MarkFrontendPhase("application bootstrap")
	beforeDiagnostics := host.diagnostics.timeoutSummary()
	host.errorSurfaceURL = "data:text/html,surface"
	if !noteFail(host, 0) {
		t.Fatal("failed document did not arm a pending fallback generation")
	}
	issueCurrentErrorSurface(t, host)

	browser := host.newWebViewBrowser()
	if browser.NavigationStartingCallback(webview2.NavigationStartingObservation{
		URI:          "",
		NavigationID: 41,
	}) {
		t.Fatal("successfully observed empty fallback start reached the origin pin")
	}
	if host.errorSurfaceNav != knownNavigationIdentity(41) {
		t.Fatalf("claimed fallback identity = %+v, want known value 41", host.errorSurfaceNav)
	}
	if host.errorSurfacePending || !host.errorSurfaceActive {
		t.Fatalf("known-empty start did not claim pending authority: pending=%t active=%t",
			host.errorSurfacePending, host.errorSurfaceActive)
	}

	logStart := len(logger.String())
	for id, control := range restrictedFallbackControls {
		browser.MessageCallback(webview2.WebMessageObservation{
			Message: `{"id":` + strconv.Quote(strconv.Itoa(id)) + `,"method":` +
				strconv.Quote(control.method) + `,"args":` + control.args + `}`,
		}, nil)
	}
	allowedLog := logger.String()[logStart:]
	if got := strings.Count(allowedLog, "bridge response sender unavailable"); got != len(restrictedFallbackControls) {
		t.Fatalf("restricted fallback responses = %d, want exactly %d:\n%s",
			got, len(restrictedFallbackControls), allowedLog)
	}
	for _, control := range restrictedFallbackControls {
		if control.log != "" && !strings.Contains(allowedLog, control.log) {
			t.Fatalf("production callback did not execute fallback control %q; log missing %q:\n%s",
				control.method, control.log, allowedLog)
		}
	}

	for _, raw := range []string{
		`{"id":"app","method":"GetSecret","args":[]}`,
		`{"id":"shell","method":"` + methodShellReady + `","args":[]}`,
		`{"id":"ready","method":"` + methodReady + `","args":[]}`,
		`{"id":"phase","method":"` + methodPhase + `","args":["fallback document created"]}`,
		`{"id":"diagnostic","method":"` + methodDiagnostic + `","args":["error","fallback diagnostic"]}`,
	} {
		browser.MessageCallback(webview2.WebMessageObservation{Message: raw}, nil)
	}
	if bridgeCalls != 0 {
		t.Fatalf("restricted fallback reached Config.Bridge %d times", bridgeCalls)
	}
	host.renderMu.Lock()
	ready, shellReady := host.frontendReady, host.frontendShellReady
	host.renderMu.Unlock()
	if ready || shellReady {
		t.Fatalf("restricted fallback changed readiness: ready=%t shellReady=%t", ready, shellReady)
	}
	if after := host.diagnostics.timeoutSummary(); after != beforeDiagnostics {
		t.Fatalf("restricted fallback changed retained diagnostics:\nbefore %q\nafter  %q",
			beforeDiagnostics, after)
	}
}

func TestWebMessageCallbackDropsFailedSourceBeforeEveryDispatcher(t *testing.T) {
	var bridgeCalls int
	host, logger := newTestHost(t, Config{
		StartHidden: true,
		Bridge: func(string) string {
			bridgeCalls++
			return ""
		},
	})
	host.errorSurfaceURL = "data:text/html,surface"
	noteFail(host, 0)
	issueCurrentErrorSurface(t, host)
	if !host.noteSurfaceNavigationStarting("", 0) {
		t.Fatal("fallback generation was not claimed")
	}
	callback := host.newWebViewBrowser().MessageCallback
	callback(webview2.WebMessageObservation{
		Message:   `{"id":"close","method":"` + methodClose + `","args":[]}`,
		SourceErr: errors.New("source unavailable"),
	}, nil)
	callback(webview2.WebMessageObservation{
		Message:   `{"id":"app","method":"GetSecret","args":[]}`,
		SourceErr: errors.New("source unavailable"),
	}, nil)

	if bridgeCalls != 0 {
		t.Fatalf("failed source reached Config.Bridge %d times", bridgeCalls)
	}
	logText := logger.String()
	if strings.Contains(logText, "quit requested") {
		t.Fatal("failed source executed WindowClose")
	}
	if got := strings.Count(logText, "event=WebMessageReceived, getter=GetSource"); got != 2 {
		t.Fatalf("source diagnostics = %d, want one per failed event:\n%s", got, logText)
	}
}

// This is the Go receipt half of scripts/test-bridge.mjs. The shipped bridge
// must post the complete detail so the bounded host reducer can still find a URL
// that begins after 240 characters; only the log boundary may reduce it.
func TestBridgeTrustedDiagnosticReceiptIsBoundedAndKeepsLateURL(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})
	detail := strings.Repeat("context ", logsafe.DiagnosticLimit) +
		"https://mullion.localhost/app/main.js?secret=value"
	raw := `{"id":"diagnostic","method":"` + methodDiagnostic +
		`","args":["error",` + strconv.Quote(detail) + `]}`

	if reply := host.handleWebMessage(raw, true); !strings.Contains(reply, `"ok":true`) {
		t.Fatalf("trusted diagnostic was not handled: %q", reply)
	}
	logText := logger.String()
	if !strings.Contains(logText, "https://mullion.localhost/app/main.js?") {
		t.Fatalf("bounded Go receipt lost the late URL:\n%s", logText)
	}
	if strings.Contains(logText, "secret=value") || strings.Contains(logText, detail) {
		t.Fatalf("bounded Go receipt retained query data or the raw detail:\n%s", logText)
	}
}
