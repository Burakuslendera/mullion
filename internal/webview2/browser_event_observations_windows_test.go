//go:build windows

package webview2

import (
	"errors"
	"strings"
	"testing"
)

func TestHandleWebMessageReceivedPreservesSourceFailure(t *testing.T) {
	args := newFakeWebMessageArgs(t, &fakeComState{
		message:      `{"method":"WindowClose"}`,
		sourceResult: eFail,
	})
	browser := New()
	var got WebMessageObservation
	var calls, reports int
	browser.MessageCallback = func(observation WebMessageObservation, _ *ICoreWebView2) {
		calls++
		got = observation
	}
	browser.ErrorCallback = func(error) { reports++ }

	browser.handleWebMessageReceived(nil, args)

	if calls != 1 {
		t.Fatalf("callback calls = %d, want 1 so the host receives source provenance", calls)
	}
	if got.Message != `{"method":"WindowClose"}` || got.SourceErr == nil {
		t.Fatalf("observation = %+v, want message plus GetSource failure", got)
	}
	if reports != 0 {
		t.Fatalf("adapter reports = %d, want 0: the host owns the one terminal source diagnostic", reports)
	}
}

func TestTryGetWebMessageAsStringDistinguishesStructuredMessages(t *testing.T) {
	for _, tc := range []struct {
		name        string
		result      uintptr
		wantReports int
	}{
		{"E_INVALIDARG structured message", 0x80070057, 0},
		{"unexpected getter failure", eFail, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := newFakeWebMessageArgs(t, &fakeComState{messageResult: tc.result})
			browser := New()
			var callbackCalls int
			var reported []error
			browser.MessageCallback = func(WebMessageObservation, *ICoreWebView2) { callbackCalls++ }
			browser.ErrorCallback = func(err error) { reported = append(reported, err) }

			browser.handleWebMessageReceived(nil, args)

			if callbackCalls != 0 {
				t.Fatalf("callback calls = %d, want 0 when the message is not a string", callbackCalls)
			}
			if len(reported) != tc.wantReports {
				t.Fatalf("reports = %d (%v), want %d", len(reported), reported, tc.wantReports)
			}
			if len(reported) == 1 && !strings.Contains(reported[0].Error(), "WebMessageReceived.TryGetWebMessageAsString") {
				t.Fatalf("unexpected failure lacks event/getter context: %v", reported[0])
			}
		})
	}
}

func requireObservationHRESULT(t *testing.T, err error, want uintptr) {
	t.Helper()
	var got HResultError
	if !errors.As(err, &got) {
		t.Fatalf("error = %v, want HRESULT 0x%08X", err, uint32(want))
	}
	if got.HResult() != uint32(want) {
		t.Fatalf("HRESULT = 0x%08X, want 0x%08X", got.HResult(), uint32(want))
	}
}

func TestEventObservationsPreserveEverySuccessfulGetterValue(t *testing.T) {
	t.Run("WebMessageReceived", func(t *testing.T) {
		const message = `{"id":"sentinel"}`
		const source = "https://source.example/sentinel"
		args := newFakeWebMessageArgs(t, &fakeComState{message: message, source: source})
		browser := New()
		var got WebMessageObservation
		var calls int
		browser.MessageCallback = func(observation WebMessageObservation, _ *ICoreWebView2) {
			calls++
			got = observation
		}

		browser.handleWebMessageReceived(nil, args)

		if calls != 1 || got.Message != message || got.Source != source || got.SourceErr != nil {
			t.Fatalf("observation = %+v, calls=%d", got, calls)
		}
	})

	t.Run("NavigationStarting", func(t *testing.T) {
		const uri = "https://start.example/sentinel"
		const navigationID = uint64(0x1122334455667788)
		state := &fakeComState{
			uri:           uri,
			navigationID:  navigationID,
			userInitiated: true,
			redirected:    true,
		}
		args := newFakeNavigationStartingArgs(t, state)
		browser := New()
		var got NavigationStartingObservation
		var calls, cancelled int
		browser.NavigationStartingCallback = func(observation NavigationStartingObservation) bool {
			calls++
			got = observation
			return true
		}
		browser.NavigationCancelledCallback = func(observation NavigationStartingObservation) {
			cancelled++
			if state.puts != 1 {
				t.Fatalf("cancel callback ran before PutCancel: puts=%d", state.puts)
			}
			if observation != got {
				t.Fatalf("cancel observation = %+v, want %+v", observation, got)
			}
		}

		browser.handleNavigationStarting(args)

		if calls != 1 || cancelled != 1 || state.puts != 1 {
			t.Fatalf("callback calls=%d, cancelled=%d, puts=%d", calls, cancelled, state.puts)
		}
		if got.URI != uri || got.NavigationID != navigationID || !got.IsUserInitiated || !got.IsRedirected ||
			got.URIErr != nil || got.NavigationIDErr != nil || got.IsUserInitiatedErr != nil || got.IsRedirectedErr != nil {
			t.Fatalf("observation = %+v", got)
		}
	})

	t.Run("NavigationCompleted", func(t *testing.T) {
		const navigationID = uint64(0x8877665544332211)
		state := &fakeComState{
			success:      true,
			status:       WebErrorStatusConnectionAborted,
			navigationID: navigationID,
		}
		args := newFakeNavigationCompletedArgs(t, state)
		browser := New()
		var got NavigationCompletedObservation
		var calls int
		browser.NavigationCompletedCallback = func(observation NavigationCompletedObservation) {
			calls++
			got = observation
		}

		browser.handleNavigationCompleted(args)

		if calls != 1 || !got.IsSuccess || got.WebErrorStatus != state.status || got.NavigationID != navigationID ||
			got.IsSuccessErr != nil || got.WebErrorStatusErr != nil || got.NavigationIDErr != nil {
			t.Fatalf("observation = %+v, calls=%d", got, calls)
		}
	})

	t.Run("NewWindowRequested", func(t *testing.T) {
		const uri = "https://new-window.example/sentinel"
		state := &fakeComState{uri: uri, userInitiated: true}
		args := newFakeNewWindowArgs(t, state)
		browser := New()
		var got NewWindowRequestedObservation
		var calls int
		browser.NewWindowRequestedCallback = func(observation NewWindowRequestedObservation) {
			calls++
			if state.puts != 1 {
				t.Fatalf("callback ran before PutHandled: puts=%d", state.puts)
			}
			got = observation
		}

		browser.handleNewWindowRequested(args)

		if calls != 1 || state.puts != 1 || got.URI != uri || !got.IsUserInitiated ||
			got.URIErr != nil || got.IsUserInitiatedErr != nil {
			t.Fatalf("observation = %+v, calls=%d, puts=%d", got, calls, state.puts)
		}
	})

	t.Run("ProcessFailed", func(t *testing.T) {
		state := &fakeComState{kind: ProcessFailedKindRenderProcessExited}
		args := newFakeProcessFailedArgs(t, state)
		browser := New()
		var got ProcessFailedObservation
		var calls int
		browser.ProcessFailedCallback = func(observation ProcessFailedObservation) {
			calls++
			got = observation
		}

		browser.handleProcessFailed(args)

		if calls != 1 || got.Kind != state.kind || got.KindErr != nil {
			t.Fatalf("observation = %+v, calls=%d", got, calls)
		}
	})
}

func TestNavigationStartingPreservesEveryGetterFailureAndCancelOrdering(t *testing.T) {
	const (
		uriFailure        = uintptr(0x80004011)
		idFailure         = uintptr(0x80004012)
		userFailure       = uintptr(0x80004013)
		redirectedFailure = uintptr(0x80004014)
	)
	state := &fakeComState{
		uri:                 "uri sentinel",
		uriResult:           uriFailure,
		navigationID:        0x1122334455667788,
		navigationIDResult:  idFailure,
		userInitiated:       true,
		userInitiatedResult: userFailure,
		redirected:          true,
		redirectedResult:    redirectedFailure,
		putResult:           sOK,
	}
	args := newFakeNavigationStartingArgs(t, state)
	browser := New()
	var got NavigationStartingObservation
	browser.NavigationStartingCallback = func(observation NavigationStartingObservation) bool {
		got = observation
		return true
	}
	var cancelled bool
	browser.NavigationCancelledCallback = func(observation NavigationStartingObservation) {
		cancelled = true
		if state.puts != 1 {
			t.Fatalf("cancel callback ran before PutCancel: puts = %d", state.puts)
		}
	}

	browser.handleNavigationStarting(args)

	if got.URI != "" || got.NavigationID != 0 || got.IsUserInitiated || got.IsRedirected {
		t.Fatalf("failed getters leaked their out-parameter values as facts: %+v", got)
	}
	requireObservationHRESULT(t, got.URIErr, uriFailure)
	requireObservationHRESULT(t, got.NavigationIDErr, idFailure)
	requireObservationHRESULT(t, got.IsUserInitiatedErr, userFailure)
	requireObservationHRESULT(t, got.IsRedirectedErr, redirectedFailure)
	if !cancelled {
		t.Fatal("successful PutCancel did not notify the host")
	}
}

func TestNavigationCompletedPreservesEveryGetterFailure(t *testing.T) {
	const (
		successFailure = uintptr(0x80004021)
		statusFailure  = uintptr(0x80004022)
		idFailure      = uintptr(0x80004023)
	)
	state := &fakeComState{
		success:            true,
		successResult:      successFailure,
		status:             WebErrorStatusConnectionAborted,
		statusResult:       statusFailure,
		navigationID:       0x8877665544332211,
		navigationIDResult: idFailure,
	}
	args := newFakeNavigationCompletedArgs(t, state)
	browser := New()
	var got NavigationCompletedObservation
	browser.NavigationCompletedCallback = func(observation NavigationCompletedObservation) { got = observation }

	browser.handleNavigationCompleted(args)

	if got.IsSuccess || got.WebErrorStatus != 0 || got.NavigationID != 0 {
		t.Fatalf("failed getters leaked their out-parameter values as facts: %+v", got)
	}
	requireObservationHRESULT(t, got.IsSuccessErr, successFailure)
	requireObservationHRESULT(t, got.WebErrorStatusErr, statusFailure)
	requireObservationHRESULT(t, got.NavigationIDErr, idFailure)
}

func TestNewWindowAndProcessObservationsPreserveGetterFailures(t *testing.T) {
	const (
		uriFailure     = uintptr(0x80004031)
		userFailure    = uintptr(0x80004032)
		processFailure = uintptr(0x80004033)
	)
	windowState := &fakeComState{
		uri:                 "new-window sentinel",
		uriResult:           uriFailure,
		userInitiated:       true,
		userInitiatedResult: userFailure,
		putResult:           sOK,
	}
	windowArgs := newFakeNewWindowArgs(t, windowState)
	browser := New()
	var window NewWindowRequestedObservation
	browser.NewWindowRequestedCallback = func(observation NewWindowRequestedObservation) {
		if windowState.puts != 1 {
			t.Fatalf("callback ran before PutHandled: puts = %d", windowState.puts)
		}
		window = observation
	}
	browser.handleNewWindowRequested(windowArgs)
	if window.URI != "" || window.IsUserInitiated {
		t.Fatalf("failed new-window getters leaked their out-parameter values as facts: %+v", window)
	}
	requireObservationHRESULT(t, window.URIErr, uriFailure)
	requireObservationHRESULT(t, window.IsUserInitiatedErr, userFailure)

	processState := &fakeComState{kind: ProcessFailedKindRenderProcessExited, kindResult: processFailure}
	processArgs := newFakeProcessFailedArgs(t, processState)
	var process ProcessFailedObservation
	browser.ProcessFailedCallback = func(observation ProcessFailedObservation) { process = observation }
	browser.handleProcessFailed(processArgs)
	if process.Kind != 0 {
		t.Fatalf("failed process getter leaked kind=%v as a fact", process.Kind)
	}
	requireObservationHRESULT(t, process.KindErr, processFailure)
}

func TestFailedEventSettersDoNotNotifyTheHost(t *testing.T) {
	t.Run("PutCancel", func(t *testing.T) {
		args := newFakeNavigationStartingArgs(t, &fakeComState{putResult: eFail})
		browser := New()
		browser.NavigationStartingCallback = func(NavigationStartingObservation) bool { return true }
		var callbacks int
		browser.NavigationCancelledCallback = func(NavigationStartingObservation) { callbacks++ }
		var warnings []error
		browser.WarningCallback = func(err error) { warnings = append(warnings, err) }

		browser.handleNavigationStarting(args)

		if callbacks != 0 {
			t.Fatal("failed PutCancel notified the host that cancellation succeeded")
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0].Error(), "NavigationStarting.PutCancel") {
			t.Fatalf("warnings = %v, want one contextual PutCancel failure", warnings)
		}
	})

	t.Run("PutHandled", func(t *testing.T) {
		args := newFakeNewWindowArgs(t, &fakeComState{putResult: eFail})
		browser := New()
		var callbacks int
		browser.NewWindowRequestedCallback = func(NewWindowRequestedObservation) { callbacks++ }
		var warnings []error
		browser.WarningCallback = func(err error) { warnings = append(warnings, err) }

		browser.handleNewWindowRequested(args)

		if callbacks != 0 {
			t.Fatal("failed PutHandled routed a second copy of the new-window request")
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0].Error(), "NewWindowRequested.PutHandled") {
			t.Fatalf("warnings = %v, want one contextual PutHandled failure", warnings)
		}
	})
}
