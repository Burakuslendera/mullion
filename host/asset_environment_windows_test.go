//go:build windows

package host

import (
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// The environment-lifetime tests (issue #161). The asset callback receives the
// WebView2 environment as an uncounted copy of the Browser's stored interface
// and runs embedder code - fs.FS reads and Logger lines - before its last COM
// use. A Logger that pumps a nested native message loop can dispatch
// WM_CLOSE/WM_DESTROY, whose teardown releases the Browser-owned reference
// before the outer callback resumes (decision 0026 records the Logger model;
// the fail-closed contract of issue #150 must keep holding through that
// schedule). The callback therefore pins the environment with a reference of
// its own for its whole duration. These tests prove the schedule with the
// counted fake from the fail-closed suite, mirroring the issue's safe
// reproduction contract: no test creates a window or enters real COM, so the
// real Runtime's refcount behavior stays a contract, not an observation.

// teardownPumpLogger is a Logger whose first line performs the teardown effect:
// one environment Release, exactly what Browser.ShuttingDown does to the
// reference it owns. It stands in for the Logger that pumps a nested message
// loop mid-callback.
type teardownPumpLogger struct {
	effect func()
	fired  bool
}

func (logger *teardownPumpLogger) pump() {
	if logger.effect != nil && !logger.fired {
		logger.fired = true
		logger.effect()
	}
}

func (logger *teardownPumpLogger) Debug(string) { logger.pump() }
func (logger *teardownPumpLogger) Info(string)  { logger.pump() }
func (logger *teardownPumpLogger) Warn(string)  { logger.pump() }
func (logger *teardownPumpLogger) Error(string) { logger.pump() }

// teardownPumpFS makes the first asset read perform the same teardown effect
// from the other caller-code seam the callback runs before its last COM use.
type teardownPumpFS struct {
	inner  fs.FS
	effect func()
	fired  bool
}

func (pump *teardownPumpFS) Open(name string) (fs.File, error) {
	if !pump.fired {
		pump.fired = true
		pump.effect()
	}
	return pump.inner.Open(name)
}

// journalingEnvironment builds the counted fake environment with a chronology
// recorder wired into its vtable slots.
func journalingEnvironment(t *testing.T) (*webview2.ICoreWebView2Environment, *assetFakeState, *[]string) {
	t.Helper()
	environment, state := newFakeAssetEnvironment(t)
	events := []string{}
	state.journal = func(event string) { events = append(events, event) }
	return environment, state, &events
}

// releaseAsTeardown performs the production teardown effect on the fake: the
// Browser-owned reference is dropped exactly the way ShuttingDown drops it.
func releaseAsTeardown(environment *webview2.ICoreWebView2Environment, state *assetFakeState) {
	state.teardownReleases++
	state.journalEvent("teardown-release")
	environment.Release()
}

// requirePinnedAndBalanced asserts the callback's ownership schedule: it took
// exactly one reference of its own, released exactly that one at exit, and
// every CreateWebResourceResponse ran while the pin stood - the callback's own
// reference covered every simulated teardown release, so no environment method
// ran on a zero/unowned reference.
func requirePinnedAndBalanced(t *testing.T, state *assetFakeState, events []string, teardownReleases int) {
	t.Helper()
	if state.addRefs != 1 {
		t.Fatalf("environment AddRef calls = %d, want exactly 1", state.addRefs)
	}
	callbackReleases := state.releases - teardownReleases
	if callbackReleases != 1 {
		t.Fatalf("callback environment Release calls = %d, want exactly 1 (total %d, teardown %d)", callbackReleases, state.releases, teardownReleases)
	}
	for _, event := range events {
		if !strings.HasPrefix(event, "create:") {
			continue
		}
		pin, err := strconv.Atoi(strings.TrimPrefix(event, "create:"))
		if err != nil {
			t.Fatalf("unparsable create event %q", event)
		}
		if pin < 1 {
			t.Fatalf("CreateWebResourceResponse ran without the callback's own reference: %v", events)
		}
	}
}

func newServingAssetProvider(assets fs.FS, log *logSink, terminal *assetTerminalRecord) assetProvider {
	return newAssetProvider(assets, log, testAssetOrigin, newNativeDiagnostics(), terminal.terminate)
}

// TestAssetCallbackPinsEnvironmentAcrossNestedTeardownFromLogger drives the
// issue's headline schedule: the callback's own Logger line pumps, the nested
// dispatch tears the Browser down (the simulated Release), and the outer
// callback then still calls CreateWebResourceResponse - on a live interface,
// because the pin was taken before the Logger ever ran.
func TestAssetCallbackPinsEnvironmentAcrossNestedTeardownFromLogger(t *testing.T) {
	resetAssetFakes(t)
	terminal := &assetTerminalRecord{}
	environment, environmentState, events := journalingEnvironment(t)
	logger := &teardownPumpLogger{effect: func() {
		releaseAsTeardown(environment, environmentState)
	}}
	provider := newServingAssetProvider(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}, newLogSink(logger), terminal)
	request, _ := newFakeAssetRequest(t)
	args, argsState := newFakeAssetArgs(t)

	provider.webResourceRequested(request, args, environment)

	requirePinnedAndBalanced(t, environmentState, *events, environmentState.teardownReleases)
	if got := len(argsState.putPointers); got != 1 {
		t.Fatalf("successful PutResponse calls = %d, want 1", got)
	}
	if responseState := assetFakeStateFor(argsState.putPointers[0]); responseState == nil || responseState.statusCode != http.StatusOK {
		t.Fatalf("the re-entered callback did not serve its own 200: %v", argsState.putPointers)
	}
	if len(terminal.stages) != 0 {
		t.Fatalf("terminate calls = %d (%v), want 0: the served response held", len(terminal.stages), terminal.stages)
	}
	if (*events)[0] != "addref" {
		t.Fatalf("chronology = %v, want the callback's own AddRef before every embedder line", *events)
	}
}

// TestAssetCallbackAnswersFailClosedWhenTeardownPrecedesResponseCreation runs
// the teardown from the fs.FS seam and then fails the served response build, so
// the fail-closed block (issue #150) must still answer through the same pinned
// environment: re-entrant teardown cannot abandon the request to the network.
func TestAssetCallbackAnswersFailClosedWhenTeardownPrecedesResponseCreation(t *testing.T) {
	resetAssetFakes(t)
	terminal := &assetTerminalRecord{}
	environment, environmentState, events := journalingEnvironment(t)
	environmentState.createFailuresLeft = 1
	assets := &teardownPumpFS{
		inner: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
		effect: func() {
			releaseAsTeardown(environment, environmentState)
		},
	}
	provider := newServingAssetProvider(assets, newLogSink(NopLogger{}), terminal)
	request, _ := newFakeAssetRequest(t)
	args, argsState := newFakeAssetArgs(t)

	provider.webResourceRequested(request, args, environment)

	requireBlockingResponse(t, argsState, environmentState)
	requirePinnedAndBalanced(t, environmentState, *events, environmentState.teardownReleases)
	if len(terminal.stages) != 0 {
		t.Fatalf("terminate calls = %d (%v), want 0: the blocking response held", len(terminal.stages), terminal.stages)
	}
	if len(environmentState.createStatuses) != 2 {
		t.Fatalf("CreateWebResourceResponse calls = %d, want 2 (served attempt, then the blocking answer)", len(environmentState.createStatuses))
	}
}

// TestAssetCallbackServesWhenDeliveredAfterTeardown covers late delivery: the
// reference that authorized the pointer is already gone when the callback
// starts. The callback takes its own reference on entry and serves in process;
// it must not lean on a reference the teardown has since released.
func TestAssetCallbackServesWhenDeliveredAfterTeardown(t *testing.T) {
	resetAssetFakes(t)
	terminal := &assetTerminalRecord{}
	environment, environmentState, events := journalingEnvironment(t)
	releaseAsTeardown(environment, environmentState)
	provider := newServingAssetProvider(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}, newLogSink(NopLogger{}), terminal)
	request, _ := newFakeAssetRequest(t)
	args, argsState := newFakeAssetArgs(t)

	provider.webResourceRequested(request, args, environment)

	requirePinnedAndBalanced(t, environmentState, *events, environmentState.teardownReleases)
	if got := len(argsState.putPointers); got != 1 {
		t.Fatalf("successful PutResponse calls = %d, want 1", got)
	}
	if responseState := assetFakeStateFor(argsState.putPointers[0]); responseState == nil || responseState.statusCode != http.StatusOK {
		t.Fatal("the late callback did not serve its own 200")
	}
	if len(terminal.stages) != 0 {
		t.Fatalf("terminate calls = %d (%v), want 0", len(terminal.stages), terminal.stages)
	}
}

// TestAssetCallbackSurvivesPanicWithThePinBalanced runs the panic exit with the
// pin in place: finish recovers, the blocking response is built through the
// still-pinned environment, and the exit leaves the reference balanced.
func TestAssetCallbackSurvivesPanicWithThePinBalanced(t *testing.T) {
	resetAssetFakes(t)
	terminal := &assetTerminalRecord{}
	environment, environmentState, events := journalingEnvironment(t)
	provider := newServingAssetProvider(panicAssetFS{}, newLogSink(NopLogger{}), terminal)
	request, _ := newFakeAssetRequest(t)
	args, argsState := newFakeAssetArgs(t)

	provider.webResourceRequested(request, args, environment)

	requireBlockingResponse(t, argsState, environmentState)
	requirePinnedAndBalanced(t, environmentState, *events, environmentState.teardownReleases)
	if len(terminal.stages) != 0 {
		t.Fatalf("terminate calls = %d (%v), want 0: the blocking response held", len(terminal.stages), terminal.stages)
	}
}

// TestAssetCallbackWithoutEnvironmentNeverTouchesTheReference pins the nil
// side of the pin: no environment object means no AddRef and no Release, and
// the exit stays the #150 escalation.
func TestAssetCallbackWithoutEnvironmentNeverTouchesTheReference(t *testing.T) {
	resetAssetFakes(t)
	terminal := &assetTerminalRecord{}
	provider := newServingAssetProvider(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}, newLogSink(NopLogger{}), terminal)
	request, _ := newFakeAssetRequest(t)
	args, argsState := newFakeAssetArgs(t)

	provider.webResourceRequested(request, args, nil)

	if len(terminal.stages) != 1 {
		t.Fatalf("terminate calls = %d, want exactly 1", len(terminal.stages))
	}
	if argsState.putCalls != 0 {
		t.Fatalf("PutResponse calls = %d, want 0", argsState.putCalls)
	}
}
