//go:build windows

package webview2

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestScriptCompletionHandlerABI(t *testing.T) {
	var v scriptCompletionVtbl
	checkVtbl(t, "scriptCompletionVtbl", unsafe.Sizeof(v), 4, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"Invoke", unsafe.Offsetof(v.Invoke), 3},
	})

	var handler scriptCompletionHandler
	if got := unsafe.Offsetof(handler.server); got != 0 {
		t.Fatalf("scriptCompletionHandler.server offset = %d, want 0", got)
	}
}

func TestScriptCompletionHandlerRegistersExactIIDAndIUnknown(t *testing.T) {
	handler := newScriptCompletionHandler()
	if handler == nil {
		t.Fatal("handler unavailable on supported test architecture")
	}
	defer handler.release()

	server := serverFor(handler.this)
	if server == nil {
		t.Fatal("script completion handler was not registered")
	}
	if server.vtbl != uintptr(unsafe.Pointer(&scriptCompletionVtable)) {
		t.Fatalf("vtable = %#x, want %#x", server.vtbl, uintptr(unsafe.Pointer(&scriptCompletionVtable)))
	}
	if server.iid != iidAddScriptToExecuteOnDocumentCreatedCompletedHandler {
		t.Fatalf("IID = %s, want %s", server.iid, iidAddScriptToExecuteOnDocumentCreatedCompletedHandler)
	}

	unknown := (*IUnknown)(unsafe.Pointer(handler))
	for _, iid := range []windows.GUID{IIDIUnknown, iidAddScriptToExecuteOnDocumentCreatedCompletedHandler} {
		pointer, err := unknown.QueryInterface(&iid)
		if err != nil {
			t.Fatalf("QueryInterface(%s): %v", iid, err)
		}
		if uintptr(pointer) != handler.this {
			t.Fatalf("QueryInterface(%s) = %#x, want %#x", iid, uintptr(pointer), handler.this)
		}
		unknown.Release()
	}
	if got := atomic.LoadInt32(&server.refs); got != 1 {
		t.Fatalf("refs after QueryInterface round trips = %d, want 1", got)
	}
}

func TestScriptCompletionInvokeUsesSlotThreeAndTreatsResultAsLPCWSTR(t *testing.T) {
	handler := newScriptCompletionHandler()
	if handler == nil {
		t.Fatal("handler unavailable on supported test architecture")
	}
	defer handler.release()

	// The callback does not dereference or AddRef result. A nonzero non-COM
	// address is sufficient to prove the handler treats it as a borrowed LPCWSTR.
	if hr := callCOMSlot(unsafe.Pointer(&scriptCompletionVtable), 3, handler.this, sOK, 1); hr != sOK {
		t.Fatalf("slot 3 Invoke = %#x, want S_OK", hr)
	}
	if err := handler.result(); err != nil {
		t.Fatalf("successful LPCWSTR completion: %v", err)
	}
	if hr := callCOMSlot(unsafe.Pointer(&scriptCompletionVtable), 3, handler.this, sOK, 1); hr != eFail {
		t.Fatalf("duplicate slot 3 Invoke = %#x, want E_FAIL", hr)
	}
	if err := handler.result(); err == nil {
		t.Fatal("duplicate completion reported success")
	}
}

func TestInitSuppliesACompletionHandlerAndAcceptsInlineSuccess(t *testing.T) {
	core, state := newFakeSurfaceCore(t, sOK)
	state.scriptInline = true
	browser := New()
	browser.core = core

	if err := browser.Init("bridge"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(state.scriptHandlers) != 1 || state.scriptHandlers[0] == 0 {
		t.Fatalf("Init handler = %#x, want one owned completion handler", state.scriptHandlers[0])
	}
	if len(browser.optionalScriptHandlers) != 0 {
		t.Fatal("inline optional completion remained retained")
	}
}

func TestInitIsNonblockingAndSealsItsRetainedHandlerOnTeardown(t *testing.T) {
	core, state := newFakeSurfaceCore(t, sOK)
	browser := New()
	browser.core = core

	if err := browser.Init("tab-flag"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(state.scriptHandlers) != 1 || state.scriptHandlers[0] == 0 {
		t.Fatalf("Init handler = %#x, want one owned completion handler", state.scriptHandlers)
	}
	if serverFor(state.scriptHandlers[0]) == nil {
		t.Fatal("Runtime-owned handler was released before its asynchronous completion")
	}

	browser.ShuttingDown()
	if got := state.completeScript(0, sOK, 1); got != eFail {
		t.Fatalf("late optional completion = %#x, want E_FAIL", got)
	}
	if serverFor(state.scriptHandlers[0]) != nil {
		t.Fatal("late optional completion left its released handler rooted")
	}
}

func TestInitReportsAsynchronousOptionalFailureAsWarning(t *testing.T) {
	core, state := newFakeSurfaceCore(t, sOK)
	browser := New()
	browser.core = core
	var warnings int
	browser.WarningCallback = func(error) { warnings++ }

	if err := browser.Init("tab-flag"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := state.completeScript(0, eFail, 0); got != sOK {
		t.Fatalf("optional completion Invoke = %#x, want S_OK", got)
	}
	if warnings != 1 {
		t.Fatalf("optional completion warnings = %d, want 1", warnings)
	}
	if len(browser.optionalScriptHandlers) != 0 {
		t.Fatal("completed optional failure remained retained")
	}
}

func TestOptionalScriptCompletionContainsWarningCallbackPanic(t *testing.T) {
	panics := captureHandlerPanics(t)
	core, state := newFakeSurfaceCore(t, sOK)
	browser := New()
	browser.core = core
	browser.WarningCallback = func(error) { panic("warning callback exploded") }

	if err := browser.Init("tab-flag"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := state.completeScript(0, eFail, 0); got != sOK {
		t.Fatalf("optional completion Invoke = %#x, want S_OK", got)
	}
	if len(*panics) != 1 || !strings.Contains((*panics)[0], "document-created script completion") {
		t.Fatalf("contained panic reports = %q, want one script-completion report", *panics)
	}
	if len(browser.optionalScriptHandlers) != 0 {
		t.Fatal("panicking optional warning remained retained")
	}
	if serverFor(state.scriptHandlers[0]) != nil {
		t.Fatal("panicking optional warning left its handler rooted")
	}
}

func TestBrowserShutdownCancelsScriptWaitImmediately(t *testing.T) {
	browser := New()
	handler := newScriptCompletionHandler()
	if handler == nil {
		t.Fatal("handler unavailable on supported test architecture")
	}
	defer handler.release()

	browser.ShuttingDown()
	if err := browser.waitForScriptCompletion(handler); err == nil {
		t.Fatal("shutdown did not cancel the required script wait")
	}
}

func TestScriptCompletionWaitCancelsAndPreservesQuitHeadlessly(t *testing.T) {
	cancelled := make(chan struct{})
	close(cancelled)
	done := make(chan struct{})
	steps, finishes := 0, 0
	probes := 0
	if _, err := waitForRequiredScriptCompletion(done, cancelled, time.Minute, "script registration", func() bool {
		probes++
		return false
	}, func() bool {
		steps++
		return false
	}, func() {
		finishes++
	}); err == nil {
		t.Fatal("cancelled wait returned nil")
	}
	if probes != 0 || steps != 0 || finishes != 1 {
		t.Fatalf("cancelled wait probes=%d steps=%d finishes=%d, want 0,0,1", probes, steps, finishes)
	}

	probes, steps, finishes = 0, 0, 0
	if _, err := waitForRequiredScriptCompletion(done, nil, time.Minute, "script registration", func() bool {
		probes++
		return false
	}, func() bool {
		steps++
		return true
	}, func() {
		finishes++
	}); err == nil {
		t.Fatal("quit wait returned nil")
	}
	if probes != 1 || steps != 1 || finishes != 1 {
		t.Fatalf("quit wait probes=%d steps=%d finishes=%d, want 1,1,1", probes, steps, finishes)
	}
}

func TestRequiredScriptWaitPrioritizesCancellationOverQueuedQuitAndReadyCompletionAndReposts(t *testing.T) {
	original := postQuitMessage
	defer func() { postQuitMessage = original }()
	var reposted uintptr
	postQuitMessage = func(code uintptr) { reposted = code }

	done := make(chan struct{})
	close(done)
	cancelled := make(chan struct{})
	messages := &pump{}
	probes, steps := 0, 0
	if _, err := waitForRequiredScriptCompletion(done, cancelled, time.Minute, "script registration", func() bool {
		probes++
		messages.quitSeen = true
		messages.quitCode = 17
		close(cancelled)
		return true
	}, func() bool {
		steps++
		return false
	}, messages.finish); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("combined cancellation/quit/completion error = %v, want cancellation", err)
	}
	if probes != 1 || steps != 0 {
		t.Fatalf("combined terminal decision probes=%d steps=%d, want 1 and 0", probes, steps)
	}
	if reposted != 17 {
		t.Fatalf("reposted quit code = %d, want 17", reposted)
	}
}

func TestRequiredScriptWaitCancelsWithoutEnteringQueueProbe(t *testing.T) {
	cancelled := make(chan struct{})
	close(cancelled)
	done := make(chan struct{})
	probes, steps, finishes := 0, 0, 0
	if _, err := waitForRequiredScriptCompletion(done, cancelled, time.Minute, "script registration", func() bool {
		probes++
		return false
	}, func() bool {
		steps++
		return false
	}, func() {
		finishes++
	}); err == nil {
		t.Fatal("cancelled required-script wait returned nil")
	}
	if probes != 0 || steps != 0 || finishes != 1 {
		t.Fatalf("cancelled required-script wait probes=%d steps=%d finishes=%d, want 0,0,1", probes, steps, finishes)
	}
}

func TestCompletionWaitPrecedenceIsDeterministicWithinOnePumpTurn(t *testing.T) {
	tests := []struct {
		name       string
		queueTurn  bool
		cancel     bool
		complete   bool
		quit       bool
		want       string
		wantResult bool
	}{
		{name: "queue cancellation beats completion", queueTurn: true, cancel: true, complete: true, want: "cancelled"},
		{name: "queue cancellation beats quit", queueTurn: true, cancel: true, quit: true, want: "cancelled"},
		{name: "queue quit beats completion", queueTurn: true, complete: true, quit: true, want: "quit"},
		{name: "step cancellation beats completion and quit", cancel: true, complete: true, quit: true, want: "cancelled"},
		{name: "step quit beats completion", complete: true, quit: true, want: "quit"},
		{name: "step completion succeeds without a terminal signal", complete: true, wantResult: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan int, 1)
			cancelled := make(chan struct{})
			acted := false
			act := func() {
				if acted {
					return
				}
				acted = true
				if tc.complete {
					done <- 42
				}
				if tc.cancel {
					close(cancelled)
				}
			}
			value, err := waitForRequiredScriptCompletion(done, cancelled, time.Minute, "script registration", func() bool {
				if tc.queueTurn {
					act()
					return tc.quit
				}
				return false
			}, func() bool {
				act()
				return tc.quit
			}, func() {})
			if tc.wantResult {
				if err != nil || value != 42 {
					t.Fatalf("completion value=%d err=%v, want 42 and nil", value, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wait error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCompletionWaitReprobesQueueAfterDeadlineBeforeTimeout(t *testing.T) {
	tests := []struct {
		name     string
		boundary func(chan int, chan struct{}) bool
		want     string
	}{
		{
			name: "cancellation beats simultaneous completion",
			boundary: func(done chan int, cancelled chan struct{}) bool {
				done <- 42
				close(cancelled)
				return false
			},
			want: "cancelled",
		},
		{
			name: "quit beats simultaneous completion",
			boundary: func(done chan int, _ chan struct{}) bool {
				done <- 42
				return true
			},
			want: "quit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan int, 1)
			cancelled := make(chan struct{})
			probes := 0
			_, err := waitForRequiredScriptCompletion(done, cancelled, 0, "script registration", func() bool {
				probes++
				if probes == 2 {
					return tc.boundary(done, cancelled)
				}
				return false
			}, func() bool {
				t.Fatal("expired wait entered the blocking pump step")
				return false
			}, func() {})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("deadline-boundary error = %v, want %q", err, tc.want)
			}
			if probes != 2 {
				t.Fatalf("queue probes = %d, want 2", probes)
			}
		})
	}
}

func TestCompletionWaitAcceptsDeadlineBoundaryCompletionBeforeTimeout(t *testing.T) {
	done := make(chan int, 1)
	probes := 0
	value, err := waitForRequiredScriptCompletion(done, nil, 0, "script registration", func() bool {
		probes++
		if probes == 2 {
			done <- 42
		}
		return false
	}, func() bool {
		t.Fatal("expired wait entered the blocking pump step")
		return false
	}, func() {})
	if err != nil || value != 42 {
		t.Fatalf("deadline-boundary completion value=%d err=%v, want 42 and nil", value, err)
	}
	if probes != 2 {
		t.Fatalf("queue probes = %d, want 2", probes)
	}
}

func TestCompletionWaitTimesOutOnlyAfterSecondEmptyQueueProbe(t *testing.T) {
	probes := 0
	_, err := waitForRequiredScriptCompletion(make(chan struct{}), nil, 0, "script registration", func() bool {
		probes++
		return false
	}, func() bool {
		t.Fatal("expired wait entered the blocking pump step")
		return false
	}, func() {})
	if err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("timeout error = %v, want gave-up result", err)
	}
	if probes != 2 {
		t.Fatalf("queue probes = %d, want 2", probes)
	}
}

func TestPumpFinishRepostsQuit(t *testing.T) {
	original := postQuitMessage
	defer func() { postQuitMessage = original }()
	var code uintptr
	postQuitMessage = func(got uintptr) { code = got }
	(&pump{quitSeen: true, quitCode: 17}).finish()
	if code != 17 {
		t.Fatalf("reposted quit code = %d, want 17", code)
	}
}

func TestRegisterDocumentCreatedScriptsDelegatesToCancellationAwareProductionWaiter(t *testing.T) {
	core, state := newFakeSurfaceCore(t, sOK)
	browser := New()
	browser.core = core
	close(browser.shutdown)

	err := browser.RegisterDocumentCreatedScripts("bridge")
	if err == nil {
		t.Fatal("closed Browser shutdown signal did not cancel production registration")
	}
	if state.operationCalls != 1 || len(state.scriptHandlers) != 1 {
		t.Fatalf("production registration calls=%d handlers=%d, want 1 and 1", state.operationCalls, len(state.scriptHandlers))
	}
	if serverFor(state.scriptHandlers[0]) == nil {
		t.Fatal("fake Runtime did not retain the cancelled completion handler")
	}
	if got := atomic.LoadInt32(&serverFor(state.scriptHandlers[0]).refs); got != 1 {
		t.Fatalf("cancelled handler refs after package release = %d, want one Runtime reference", got)
	}
	if got := state.completeScript(0, sOK, 1); got != eFail {
		t.Fatalf("late cancelled completion = %#x, want E_FAIL", got)
	}
	if serverFor(state.scriptHandlers[0]) != nil {
		t.Fatal("late cancelled completion left its Runtime reference rooted")
	}
}

func TestRegisterDocumentCreatedScriptsWaitsBeforeStartingEachDependentRegistration(t *testing.T) {
	core, state := newFakeSurfaceCore(t, sOK)
	browser := New()
	browser.core = core

	var waits int
	err := browser.registerDocumentCreatedScriptsWithWait([]string{"bridge", "diagnostics", "drag", "resize"}, func(handler *scriptCompletionHandler) error {
		waits++
		if got := len(state.scriptHandlers); got != waits {
			t.Fatalf("registrations started before wait %d = %d, want %d", waits, got, waits)
		}
		if got := state.scriptHandlers[waits-1]; got != handler.this {
			t.Fatalf("wait %d handler = %#x, want current registration %#x", waits, handler.this, got)
		}
		server := serverFor(handler.this)
		if server == nil {
			t.Fatalf("wait %d handler is not rooted", waits)
		}
		if got := atomic.LoadInt32(&server.refs); got != 2 {
			t.Fatalf("wait %d handler refs before completion = %d, want package plus Runtime", waits, got)
		}
		if hr := state.completeScript(waits-1, sOK, 1); hr != sOK {
			return errors.New("fake completion rejected")
		}
		if got := atomic.LoadInt32(&server.refs); got != 1 {
			t.Fatalf("wait %d handler refs after completion = %d, want package reference", waits, got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("register document-created scripts: %v", err)
	}
	if waits != 4 {
		t.Fatalf("completion waits = %d, want 4", waits)
	}
	for index, handler := range state.scriptHandlers {
		if serverFor(handler) != nil {
			t.Fatalf("completed handler %d remained rooted after package release", index+1)
		}
	}
}

func TestStaleRequiredScriptCallbackCannotCompleteFreshRetry(t *testing.T) {
	core, state := newFakeSurfaceCore(t, sOK)
	browser := New()
	browser.core = core

	firstErr := browser.registerDocumentCreatedScriptsWithWait([]string{"bridge"}, func(*scriptCompletionHandler) error {
		return errors.New("cancel first attempt")
	})
	if firstErr == nil {
		t.Fatal("cancelled first attempt returned nil")
	}
	if len(state.scriptHandlers) != 1 {
		t.Fatalf("first-attempt handlers = %d, want one", len(state.scriptHandlers))
	}
	stale := serverFor(state.scriptHandlers[0])
	if stale == nil || atomic.LoadInt32(&stale.refs) != 1 {
		t.Fatalf("stale handler refs = %v, want one Runtime reference", stale)
	}

	err := browser.registerDocumentCreatedScriptsWithWait([]string{"bridge"}, func(fresh *scriptCompletionHandler) error {
		if len(state.scriptHandlers) != 2 || state.scriptHandlers[1] != fresh.this {
			t.Fatalf("fresh handler registration = %#x, want second handler %#x", state.scriptHandlers, fresh.this)
		}
		if got := state.completeScript(0, sOK, 1); got != eFail {
			t.Fatalf("stale first-attempt callback = %#x, want E_FAIL", got)
		}
		select {
		case <-fresh.done:
			t.Fatal("stale first-attempt callback completed the fresh retry")
		default:
		}
		freshServer := serverFor(fresh.this)
		if freshServer == nil || atomic.LoadInt32(&freshServer.refs) != 2 {
			t.Fatalf("fresh handler refs before its callback = %v, want package plus Runtime", freshServer)
		}
		if got := state.completeScript(1, sOK, 1); got != sOK {
			return fmt.Errorf("complete fresh retry: HRESULT %#x", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("fresh retry: %v", err)
	}
	for index, handler := range state.scriptHandlers {
		if serverFor(handler) != nil {
			t.Fatalf("attempt handler %d remained rooted after callbacks", index+1)
		}
	}
}

func TestRegisterDocumentCreatedScriptsFailureNPreventsRegistrationNPlusOne(t *testing.T) {
	for failureIndex := 0; failureIndex < 4; failureIndex++ {
		t.Run(fmt.Sprintf("synchronous script %d", failureIndex+1), func(t *testing.T) {
			core, state := newFakeSurfaceCore(t, sOK)
			state.scriptResults = make([]uintptr, failureIndex+1)
			for index := range state.scriptResults {
				state.scriptResults[index] = sOK
			}
			state.scriptResults[failureIndex] = eFail
			browser := New()
			browser.core = core
			waits := 0
			err := browser.registerDocumentCreatedScriptsWithWait([]string{"bridge", "diagnostics", "drag", "resize"}, func(handler *scriptCompletionHandler) error {
				index := waits
				waits++
				if hr := state.completeScript(index, sOK, 1); hr != sOK {
					return fmt.Errorf("complete script %d: HRESULT %#x", index+1, hr)
				}
				return nil
			})
			if err == nil {
				t.Fatal("synchronous failure returned nil")
			}
			if got := len(state.scriptHandlers); got != failureIndex+1 {
				t.Fatalf("registrations after script %d failed = %d, want %d", failureIndex+1, got, failureIndex+1)
			}
			if waits != failureIndex {
				t.Fatalf("waits before script %d synchronous failure = %d, want %d", failureIndex+1, waits, failureIndex)
			}
		})

		t.Run(fmt.Sprintf("asynchronous script %d", failureIndex+1), func(t *testing.T) {
			core, state := newFakeSurfaceCore(t, sOK)
			browser := New()
			browser.core = core
			waits := 0
			err := browser.registerDocumentCreatedScriptsWithWait([]string{"bridge", "diagnostics", "drag", "resize"}, func(handler *scriptCompletionHandler) error {
				index := waits
				waits++
				hr := uintptr(sOK)
				if index == failureIndex {
					hr = eFail
				}
				if got := state.scriptHandlers[index]; got != handler.this {
					t.Fatalf("wait %d handler = %#x, want %#x", index+1, handler.this, got)
				}
				if result := state.completeScript(index, hr, 1); result != sOK {
					return fmt.Errorf("complete script %d: HRESULT %#x", index+1, result)
				}
				return nil
			})
			if err == nil {
				t.Fatal("asynchronous failure returned nil")
			}
			if got := len(state.scriptHandlers); got != failureIndex+1 {
				t.Fatalf("registrations after script %d failed = %d, want %d", failureIndex+1, got, failureIndex+1)
			}
			if waits != failureIndex+1 {
				t.Fatalf("waits through script %d asynchronous failure = %d, want %d", failureIndex+1, waits, failureIndex+1)
			}
		})
	}
}

func TestRequiredScriptBarrierCountsFakeRuntimeAndPumpEffectsWithoutForbiddenFollowOnAdd(t *testing.T) {
	tests := []struct {
		name        string
		failureAt   int
		wantErr     bool
		wantEffects int
	}{
		{name: "first completion fails", failureAt: 0, wantErr: true, wantEffects: 1},
		{name: "all completions succeed", failureAt: -1, wantEffects: 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			core, state := newFakeSurfaceCore(t, sOK)
			browser := New()
			browser.core = core
			pumpSteps, pumpFinishes := 0, 0

			err := browser.registerDocumentCreatedScriptsWithWait(
				[]string{"bridge", "diagnostics", "drag", "resize"},
				func(handler *scriptCompletionHandler) error {
					index := pumpSteps
					_, waitErr := waitForRequiredScriptCompletion(
						handler.done,
						browser.shutdown,
						time.Second,
						"counted fake Runtime completion",
						func() bool { return false },
						func() bool {
							pumpSteps++
							completionResult := uintptr(sOK)
							if index == tc.failureAt {
								completionResult = eFail
							}
							if got := state.completeScript(index, completionResult, 1); got != sOK {
								t.Fatalf("fake Runtime completion %d Invoke = %#x, want S_OK", index+1, got)
							}
							return false
						},
						func() { pumpFinishes++ },
					)
					return waitErr
				},
			)
			if (err != nil) != tc.wantErr {
				t.Fatalf("registration error = %v, wantErr=%v", err, tc.wantErr)
			}
			if state.operationCalls != tc.wantEffects || pumpSteps != tc.wantEffects || pumpFinishes != tc.wantEffects {
				t.Fatalf("effects Runtime_Add=%d pump_step=%d pump_finish=%d, want %d each", state.operationCalls, pumpSteps, pumpFinishes, tc.wantEffects)
			}
			if got := len(state.scriptHandlers); got != tc.wantEffects {
				t.Fatalf("started required registrations = %d, want %d", got, tc.wantEffects)
			}
		})
	}
}

func TestRegisterDocumentCreatedScriptsRejectsInvalidDuplicateAndAbandonedCompletions(t *testing.T) {
	tests := []struct {
		name string
		wait func(*fakeComState, *scriptCompletionHandler) error
	}{
		{
			name: "invalid success result",
			wait: func(state *fakeComState, _ *scriptCompletionHandler) error {
				state.completeScript(0, sOK, 0)
				return nil
			},
		},
		{
			name: "duplicate completion",
			wait: func(state *fakeComState, _ *scriptCompletionHandler) error {
				state.completeScript(0, sOK, 1)
				state.completeScript(0, sOK, 1)
				return nil
			},
		},
		{
			name: "abandoned callback",
			wait: func(*fakeComState, *scriptCompletionHandler) error { return errors.New("cancelled") },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			core, state := newFakeSurfaceCore(t, sOK)
			browser := New()
			browser.core = core
			err := browser.registerDocumentCreatedScriptsWithWait([]string{"bridge", "diagnostics", "drag", "resize"}, func(handler *scriptCompletionHandler) error {
				return tc.wait(state, handler)
			})
			if err == nil {
				t.Fatal("registration returned nil, want terminal failure")
			}
			if got := len(state.scriptHandlers); got != 1 {
				t.Fatalf("registrations after first terminal completion = %d, want 1", got)
			}
			if tc.name == "abandoned callback" {
				if serverFor(state.scriptHandlers[0]) == nil {
					t.Fatal("Runtime-owned stale handler was not rooted")
				}
				if got := state.completeScript(0, sOK, 1); got != eFail {
					t.Fatalf("stale callback after abandonment = %#x, want E_FAIL", got)
				}
			}
		})
	}
}

func TestRegisterDocumentCreatedScriptsRejectsDuplicateDeliveredWhileWaitingForALaterScript(t *testing.T) {
	core, state := newFakeSurfaceCore(t, sOK)
	browser := New()
	browser.core = core
	var waits int

	err := browser.registerDocumentCreatedScriptsWithWait([]string{"bridge", "diagnostics", "drag", "resize"}, func(handler *scriptCompletionHandler) error {
		waits++
		switch waits {
		case 1:
			state.completeScript(0, sOK, 1)
		case 2:
			state.completeScript(0, sOK, 1)
			state.completeScript(1, sOK, 1)
		}
		if got := state.scriptHandlers[waits-1]; got != handler.this {
			t.Fatalf("wait %d handler = %#x, want %#x", waits, handler.this, got)
		}
		return nil
	})
	if err == nil {
		t.Fatal("duplicate delivered during a later wait returned nil")
	}
	if got := len(state.scriptHandlers); got != 2 {
		t.Fatalf("registrations after prior duplicate = %d, want 2", got)
	}
}
