//go:build windows

package host

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"unsafe"

	"github.com/Burakuslendera/mullion/internal/webview2"
	"golang.org/x/sys/windows"
)

// The fail-closed contract tests (issue #150). WebView2 continues a
// WebResourceRequested event that ends without put_Response on the normal
// network stack, so every injected failure - the getters, the environment, the
// response construction, PutResponse itself, and a panic - must leave either a
// deterministic blocking response installed or the terminal teardown latched.
// The COM objects are fakes built on the exported vtable structs, mirroring the
// package's own fakes; no test here touches a runtime or a socket.

const (
	assetFakeOK   = uintptr(0)
	assetFakeFail = uintptr(0x80004005)
)

var (
	assetFakeCoTaskMemAlloc = windows.NewLazySystemDLL("ole32.dll").NewProc("CoTaskMemAlloc")
	assetFakeRtlMoveMemory  = windows.NewLazySystemDLL("kernel32.dll").NewProc("RtlMoveMemory")
)

// assetFakeState is the per-object record the fake vtable slots read and write.
// One struct serves every fake kind; unused fields stay zero.
type assetFakeState struct {
	uri, method        string
	failURI            bool
	failMethod         bool
	putCalls           int
	putFailuresLeft    int
	putPointers        []uintptr
	createCalls        int
	createStatuses     []int32
	createReasons      []string
	createHeaders      []string
	createFailuresLeft int
	statusCode         int32
	putContentCalls    int
	releases           int
}

var (
	assetFakeMu     sync.Mutex
	assetFakeStates = map[uintptr]*assetFakeState{}
	assetFakeKeep   []any
)

// resetAssetFakes clears the registry before and after each test. Package tests
// run sequentially, so a shared registry is safe; the keep slice holds the fake
// wrappers reachable for the test's lifetime so a freed address cannot be
// reused by a later allocation and misread.
func resetAssetFakes(t *testing.T) {
	t.Helper()
	clear := func() {
		assetFakeMu.Lock()
		assetFakeStates = map[uintptr]*assetFakeState{}
		assetFakeKeep = nil
		assetFakeMu.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

func assetFakeTrack[T any](object *T, state *assetFakeState) {
	assetFakeMu.Lock()
	defer assetFakeMu.Unlock()
	assetFakeStates[uintptr(unsafe.Pointer(object))] = state
	assetFakeKeep = append(assetFakeKeep, object)
}

func assetFakeStateFor(this uintptr) *assetFakeState {
	assetFakeMu.Lock()
	defer assetFakeMu.Unlock()
	return assetFakeStates[this]
}

// assetFakeWriteAddress stores one machine word into an out-parameter supplied
// through the fake vtable, mirroring the package's writeAddress idiom.
func assetFakeWriteAddress(dst, value uintptr) {
	if dst == 0 {
		return
	}
	stored := value
	_, _, _ = assetFakeRtlMoveMemory.Call(dst, uintptr(unsafe.Pointer(&stored)), unsafe.Sizeof(stored))
}

// assetFakeUtf16At reads a NUL-terminated UTF-16 string out of fake-owned
// memory by moving one unit at a time, mirroring the package's utf16At idiom.
func assetFakeUtf16At(address uintptr) string {
	const limit = 4096
	units := make([]uint16, 0, 64)
	for offset := uintptr(0); offset < limit; offset += 2 {
		var unit uint16
		_, _, _ = assetFakeRtlMoveMemory.Call(
			uintptr(unsafe.Pointer(&unit)),
			address+offset,
			unsafe.Sizeof(unit),
		)
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	return windows.UTF16ToString(units)
}

// assetFakeWriteWstr hands a string out the way COM expects: CoTaskMem memory
// the caller frees.
func assetFakeWriteWstr(out uintptr, value string) {
	encoded, err := windows.UTF16FromString(value)
	if err != nil {
		panic(err)
	}
	size := uintptr(len(encoded)) * unsafe.Sizeof(encoded[0])
	memory, _, _ := assetFakeCoTaskMemAlloc.Call(size)
	if memory == 0 {
		panic("CoTaskMemAlloc failed")
	}
	_, _, _ = assetFakeRtlMoveMemory.Call(memory, uintptr(unsafe.Pointer(&encoded[0])), size)
	assetFakeWriteAddress(out, memory)
}

var assetFakeRequestVtbl = webview2.ICoreWebView2WebResourceRequestVtbl{
	GetUri: webview2.ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := assetFakeStateFor(this)
		if state == nil || state.failURI {
			return assetFakeFail
		}
		assetFakeWriteWstr(out, state.uri)
		return assetFakeOK
	})),
	GetMethod: webview2.ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := assetFakeStateFor(this)
		if state == nil || state.failMethod {
			return assetFakeFail
		}
		assetFakeWriteWstr(out, state.method)
		return assetFakeOK
	})),
}

var assetFakeArgsVtbl = webview2.ICoreWebView2WebResourceRequestedEventArgsVtbl{
	PutResponse: webview2.ComProc(windows.NewCallback(func(this, response uintptr) uintptr {
		state := assetFakeStateFor(this)
		if state == nil {
			return assetFakeFail
		}
		state.putCalls++
		if state.putFailuresLeft > 0 {
			state.putFailuresLeft--
			return assetFakeFail
		}
		state.putPointers = append(state.putPointers, response)
		return assetFakeOK
	})),
}

var assetFakeEnvironmentVtbl = webview2.ICoreWebView2EnvironmentVtbl{
	CreateWebResourceResponse: webview2.ComProc(windows.NewCallback(func(this, content, statusCode, reason, headers, out uintptr) uintptr {
		state := assetFakeStateFor(this)
		if state == nil {
			return assetFakeFail
		}
		state.createCalls++
		state.createStatuses = append(state.createStatuses, int32(statusCode))
		state.createReasons = append(state.createReasons, assetFakeUtf16At(reason))
		state.createHeaders = append(state.createHeaders, assetFakeUtf16At(headers))
		if state.createFailuresLeft > 0 {
			state.createFailuresLeft--
			return assetFakeFail
		}
		response := &webview2.ICoreWebView2WebResourceResponse{Vtbl: &assetFakeResponseVtbl}
		assetFakeTrack(response, &assetFakeState{statusCode: int32(statusCode)})
		assetFakeWriteAddress(out, uintptr(unsafe.Pointer(response)))
		return assetFakeOK
	})),
}

var assetFakeResponseVtbl = webview2.ICoreWebView2WebResourceResponseVtbl{
	IUnknownVtbl: webview2.IUnknownVtbl{
		Release: webview2.ComProc(windows.NewCallback(func(this uintptr) uintptr {
			state := assetFakeStateFor(this)
			if state != nil {
				state.releases++
			}
			return 1
		})),
	},
	PutContent: webview2.ComProc(windows.NewCallback(func(this, content uintptr) uintptr {
		state := assetFakeStateFor(this)
		if state == nil {
			return assetFakeFail
		}
		state.putContentCalls++
		return assetFakeOK
	})),
}

func newFakeAssetRequest(t *testing.T) (*webview2.ICoreWebView2WebResourceRequest, *assetFakeState) {
	t.Helper()
	request := &webview2.ICoreWebView2WebResourceRequest{Vtbl: &assetFakeRequestVtbl}
	state := &assetFakeState{uri: testOrigin + "/", method: "GET"}
	assetFakeTrack(request, state)
	return request, state
}

func newFakeAssetArgs(t *testing.T) (*webview2.ICoreWebView2WebResourceRequestedEventArgs, *assetFakeState) {
	t.Helper()
	args := &webview2.ICoreWebView2WebResourceRequestedEventArgs{Vtbl: &assetFakeArgsVtbl}
	state := &assetFakeState{}
	assetFakeTrack(args, state)
	return args, state
}

func newFakeAssetEnvironment(t *testing.T) (*webview2.ICoreWebView2Environment, *assetFakeState) {
	t.Helper()
	environment := &webview2.ICoreWebView2Environment{Vtbl: &assetFakeEnvironmentVtbl}
	state := &assetFakeState{}
	assetFakeTrack(environment, state)
	return environment, state
}

// assetTerminalRecord is the provider's terminate seam for tests.
type assetTerminalRecord struct {
	stages []string
	causes []error
}

func (record *assetTerminalRecord) terminate(stage string, cause error) {
	record.stages = append(record.stages, stage)
	record.causes = append(record.causes, cause)
}

// panicAssetFS makes the resolve path panic, the one failure class that is not
// an error return.
type panicAssetFS struct{}

func (panicAssetFS) Open(string) (fs.File, error) {
	panic("asset fs exploded")
}

// requireBlockingResponse asserts the deterministic blocking answer reached the
// event args: a 500 with the boundary's standard headers, built with no
// content, put exactly once, and its creator reference released. Rows that
// first fail a served attempt will show earlier create/put calls; only the
// last create is the blocking one.
func requireBlockingResponse(t *testing.T, argsState, environmentState *assetFakeState) {
	t.Helper()
	if environmentState.createCalls == 0 {
		t.Fatal("CreateWebResourceResponse was never called")
	}
	last := len(environmentState.createStatuses) - 1
	if got := environmentState.createStatuses[last]; got != http.StatusInternalServerError {
		t.Fatalf("blocking response status = %d, want %d", got, http.StatusInternalServerError)
	}
	if got := environmentState.createReasons[last]; got != http.StatusText(http.StatusInternalServerError) {
		t.Fatalf("blocking response reason = %q, want %q", got, http.StatusText(http.StatusInternalServerError))
	}
	if !strings.Contains(environmentState.createHeaders[last], "X-Content-Type-Options: nosniff") {
		t.Fatalf("blocking response headers = %q, want nosniff", environmentState.createHeaders[last])
	}
	if !strings.Contains(environmentState.createHeaders[last], "Cache-Control: no-store") {
		t.Fatalf("blocking response headers = %q, want no-store", environmentState.createHeaders[last])
	}
	if got := len(argsState.putPointers); got != 1 {
		t.Fatalf("successful PutResponse calls = %d, want 1", got)
	}
	responseState := assetFakeStateFor(argsState.putPointers[0])
	if responseState == nil || responseState.statusCode != http.StatusInternalServerError {
		t.Fatalf("PutResponse received status %v, want the blocking 500", responseState)
	}
	if responseState.releases != 1 {
		t.Fatalf("blocking response creator releases = %d, want 1: the runtime owns the only other reference", responseState.releases)
	}
}

// TestAssetCallbackFailsClosedOnEveryErrorExit injects each recoverable failure
// independently and asserts the blocking response was installed, so no error or
// panic exit can hand the matched request to the network (issue #150).
func TestAssetCallbackFailsClosedOnEveryErrorExit(t *testing.T) {
	tests := []struct {
		name       string
		nilRequest bool
		request    func(*assetFakeState)
		args       func(*assetFakeState)
		env        func(*assetFakeState)
		assets     fs.FS
	}{
		{
			// The webview2 layer forwards a failed GetRequest as a nil request.
			name:       "request unavailable",
			nilRequest: true,
		},
		{
			name:    "get uri failed",
			request: func(state *assetFakeState) { state.failURI = true },
		},
		{
			name: "create response failed once",
			env:  func(state *assetFakeState) { state.createFailuresLeft = 1 },
		},
		{
			name: "put response failed once",
			args: func(state *assetFakeState) { state.putFailuresLeft = 1 },
		},
		{
			name:   "panic in the asset fs",
			assets: panicAssetFS{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetAssetFakes(t)
			assets := test.assets
			if assets == nil {
				assets = fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
			}
			terminal := &assetTerminalRecord{}
			provider := newAssetProvider(assets, newLogSink(NopLogger{}), testAssetOrigin, newNativeDiagnostics(), terminal.terminate)
			request, requestState := newFakeAssetRequest(t)
			args, argsState := newFakeAssetArgs(t)
			environment, environmentState := newFakeAssetEnvironment(t)
			if test.request != nil {
				test.request(requestState)
			}
			if test.args != nil {
				test.args(argsState)
			}
			if test.env != nil {
				test.env(environmentState)
			}

			callbackRequest := request
			if test.nilRequest {
				callbackRequest = nil
			}
			provider.webResourceRequested(callbackRequest, args, environment)

			requireBlockingResponse(t, argsState, environmentState)
			if len(terminal.stages) != 0 {
				t.Fatalf("terminate calls = %d (%v), want 0: the blocking response held", len(terminal.stages), terminal.stages)
			}
		})
	}
}

// TestAssetCallbackEscalatesWhenNoResponseIsPossible covers the exits where no
// blocking response can exist - no args to answer on, no environment to build
// one from, or a runtime that refuses both attempts - and asserts the terminal
// teardown is the recorded outcome (issue #150).
func TestAssetCallbackEscalatesWhenNoResponseIsPossible(t *testing.T) {
	tests := []struct {
		name     string
		args     func(*assetFakeState)
		env      func(*assetFakeState)
		putCalls int
	}{
		{name: "event args unavailable", putCalls: 0},
		{name: "environment unavailable", putCalls: 0},
		{
			name:     "create response never succeeds",
			env:      func(state *assetFakeState) { state.createFailuresLeft = 2 },
			putCalls: 0,
		},
		{
			name:     "put response never succeeds",
			args:     func(state *assetFakeState) { state.putFailuresLeft = 2 },
			putCalls: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetAssetFakes(t)
			terminal := &assetTerminalRecord{}
			provider := newAssetProvider(fstest.MapFS{
				"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
			}, newLogSink(NopLogger{}), testAssetOrigin, newNativeDiagnostics(), terminal.terminate)
			request, _ := newFakeAssetRequest(t)
			args, argsState := newFakeAssetArgs(t)
			environment, environmentState := newFakeAssetEnvironment(t)
			if test.args != nil {
				test.args(argsState)
			}
			if test.env != nil {
				test.env(environmentState)
			}

			// A nil args or environment stands in for its own unavailable case;
			// both are escalated, never answered.
			providerArgs := args
			providerEnvironment := environment
			if test.name == "event args unavailable" {
				providerArgs = nil
			}
			if test.name == "environment unavailable" {
				providerEnvironment = nil
			}

			provider.webResourceRequested(request, providerArgs, providerEnvironment)

			if len(terminal.stages) != 1 {
				t.Fatalf("terminate calls = %d, want exactly 1", len(terminal.stages))
			}
			if terminal.stages[0] == "" {
				t.Fatal("terminate recorded no stage")
			}
			if terminal.causes[0] == nil {
				t.Fatal("terminate recorded no cause")
			}
			if got := argsState.putCalls; got != test.putCalls {
				t.Fatalf("PutResponse calls = %d, want %d", got, test.putCalls)
			}
		})
	}
}

// TestAssetCallbackServesWithoutEscalation pins the success path against the
// fail-closed rework: a normal document is still answered with its own 200
// response, an unreadable method is absorbed exactly as before, and the
// terminal seam stays silent.
func TestAssetCallbackServesWithoutEscalation(t *testing.T) {
	resetAssetFakes(t)
	terminal := &assetTerminalRecord{}
	provider := newAssetProvider(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}, newLogSink(NopLogger{}), testAssetOrigin, newNativeDiagnostics(), terminal.terminate)
	request, requestState := newFakeAssetRequest(t)
	args, argsState := newFakeAssetArgs(t)
	environment, environmentState := newFakeAssetEnvironment(t)
	requestState.failMethod = true

	provider.webResourceRequested(request, args, environment)

	if got := len(argsState.putPointers); got != 1 {
		t.Fatalf("successful PutResponse calls = %d, want 1", got)
	}
	if got := environmentState.createStatuses[0]; got != http.StatusOK {
		t.Fatalf("served status = %d, want 200", got)
	}
	if responseState := assetFakeStateFor(argsState.putPointers[0]); responseState == nil || responseState.statusCode != http.StatusOK {
		t.Fatal("the runtime received something other than the served response")
	}
	if responseState := assetFakeStateFor(argsState.putPointers[0]); responseState.putContentCalls != 1 {
		t.Fatalf("PutContent calls = %d, want 1: the body stream must be attached", responseState.putContentCalls)
	}
	if len(terminal.stages) != 0 {
		t.Fatalf("terminate calls = %d, want 0", len(terminal.stages))
	}
}

// TestAssetBoundaryTerminalLatchesOnceAndRefusesShuttingDown pins the host-side
// escalation: exactly one latched cause per Run, the message loop's outcome is
// ErrAssetBoundaryClosed rather than a normal close, a shutting-down browser
// refuses the request, and the shared teardown command logs the true cause.
func TestAssetBoundaryTerminalLatchesOnceAndRefusesShuttingDown(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	if host.browserExitTerminalOutcome() != nil {
		t.Fatal("a fresh host must not report a terminal outcome")
	}
	host.requestAssetBoundaryTerminal("environment", errAssetEnvironmentUnavailable)
	host.requestAssetBoundaryTerminal("environment", errAssetEnvironmentUnavailable)
	if !host.assetBoundaryTerminal {
		t.Fatal("the escalation did not latch")
	}
	if !errors.Is(host.browserExitTerminalOutcome(), ErrAssetBoundaryClosed) {
		t.Fatalf("outcome = %v, want ErrAssetBoundaryClosed", host.browserExitTerminalOutcome())
	}
	if got := strings.Count(logger.String(), "asset boundary terminal requested"); got != 1 {
		t.Fatalf("terminal requested lines = %d, want exactly 1", got)
	}

	shuttingDownHost, shuttingDownLogger := newTestHost(t, Config{})
	browser := webview2.New()
	browser.ShuttingDown()
	shuttingDownHost.browser = browser
	shuttingDownHost.requestAssetBoundaryTerminal("environment", errAssetEnvironmentUnavailable)
	if shuttingDownHost.assetBoundaryTerminal {
		t.Fatal("a shutting-down browser latched the boundary terminal")
	}
	if strings.Contains(shuttingDownLogger.String(), "asset boundary terminal requested") {
		t.Fatal("a shutting-down browser scheduled the boundary teardown")
	}
}

// TestAssetBoundaryTerminalAppliesThroughTheTaggedCommand proves the escalation
// reaches the window destroy through the same token-validated dispatch as the
// browser-process-exit policy, and that the log names the asset cause.
func TestAssetBoundaryTerminalAppliesThroughTheTaggedCommand(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	const hwnd = windowHandle(0x1500)
	run := beginHeadlessLifecycleRun(t, host, hwnd)

	host.requestAssetBoundaryTerminal("environment", errAssetEnvironmentUnavailable)
	host.windowProc(hwnd, wmNativeProcessExit, 0, run.token)
	if !strings.Contains(logger.String(), "asset boundary terminal applying") {
		t.Fatalf("the tagged command did not apply the boundary teardown:\n%s", logger.String())
	}
	if strings.Contains(logger.String(), "browser process exit terminal applying") {
		t.Fatalf("the asset escalation was logged as a browser process exit:\n%s", logger.String())
	}
}
