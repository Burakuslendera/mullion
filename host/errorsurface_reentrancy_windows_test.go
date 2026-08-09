//go:build windows

package host

import (
	"errors"
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// Decision 0026's ordering rule, driven rather than described: every report
// comes after the state transition it describes, never before.
//
// The rule exists because a log call hands control to the embedder's Logger,
// which is arbitrary code. A Logger that pumps messages - a MessageBox, a GUI
// toolkit's own loop - has a queued navigation completion dispatched inside it,
// re-entering this machine. Report first and that nested completion is
// classified against a machine that has not transitioned yet: it arms a whole
// generation of its own, and the outer writes then land on top of what it
// claimed.
//
// 0026 wrote the rule and applied it at both of its own sites, and locked
// neither. An audit put the warning back in front of the four state writes in
// armErrorSurface, and in front of the seal's admission drop in
// noteSurfaceOwnOutcome, with the entire suite green both times - while the same
// rule in the cancelled-navigation ledger next door was driven from the start by
// TestEvictionSurvivesALoggerThatReentersTheLedger. These two are the
// error-surface machine's half of it.
//
// reentrantLogger, the Logger that runs a hook from inside Warn, is defined with
// that test in navigationledger_windows_test.go.

// newReentrantSurfaceHost is newSurfaceHost with a Logger that re-enters. The
// serving mode is deliberately the same one: with Config.URL set, a
// ConnectionAborted completion is a real failure rather than a benign abort
// (decisions/0024), and both cases below need a failure that actually arms.
func newReentrantSurfaceHost(t *testing.T) (*Host, *reentrantLogger) {
	t.Helper()
	logger := &reentrantLogger{captureLogger: &captureLogger{}}
	host := New(Config{URL: testExternalURL, Logger: logger})
	stubExternalOpen(host)
	return host, logger
}

// Classification publishes only a revocable plan before reporting. A nested
// failure is absorbed against that plan, but pending/loading stay closed until
// production is immediately ready to call Navigate.
func TestClassificationIsReportedWithoutOpeningAClaimWindow(t *testing.T) {
	host, logger := newReentrantSurfaceHost(t)

	var nestedAskedToShow bool
	logger.onWarn = func() { nestedAskedToShow = noteFail(host, 43) }

	if !noteFail(host, 42) {
		t.Fatal("the arming failure must create a fallback plan")
	}
	if nestedAskedToShow {
		t.Fatal("a completion re-entering the classification report created competing fallback authority")
	}
	if host.errorSurfacePlan == noErrorSurfacePlan ||
		host.errorSurfacePending || host.errorSurfaceLoading {
		t.Fatal("classification did not retain exactly one unissued fallback plan")
	}
}

// The seal site. The surface's own load dying drops the admission and then says
// so. A nested completion inside that warning arms a fresh pending generation,
// and that pending state has to survive the rest of the outer call - report
// first and the outer cleanup lands on top of it, losing the generation.
func TestTheSealDropsTheAdmissionBeforeItIsReported(t *testing.T) {
	host, logger := newReentrantSurfaceHost(t)
	armAndClaim(t, host, 5, 6)

	logger.onWarn = func() {
		if !noteFail(host, 7) {
			t.Error("the nested failure must arm a generation of its own, or this case asserts nothing")
		}
	}

	if noteFail(host, 6) {
		t.Fatal("the surface's own load failing must not ask for another navigation")
	}
	if host.errorSurfacePlan == noErrorSurfacePlan ||
		host.errorSurfacePending || host.errorSurfaceActive {
		t.Fatal("the nested fallback plan was overwritten or issued by classification")
	}
}

type debugReentrantLogger struct {
	*captureLogger
	onDebug func()
}

func (logger *debugReentrantLogger) Debug(message string) {
	logger.captureLogger.Debug(message)
	if hook := logger.onDebug; hook != nil {
		logger.onDebug = nil
		hook()
	}
}

func TestDepartureRestorationIsCommittedBeforeItIsReported(t *testing.T) {
	t.Run("benign abort", func(t *testing.T) {
		logger := &debugReentrantLogger{captureLogger: &captureLogger{}}
		host := New(Config{Logger: logger})
		stubExternalOpen(host)
		host.noteNavigationOutcome(false, statusNone, 1)
		issueCurrentErrorSurface(t, host)
		host.noteSurfaceNavigationStarting(host.errorSurfaceURL, 2)
		noteOK(host, 2)
		host.noteAndGateNavigation(host.source.origin.text+"/index.html", 3)

		var admittedInsideLog bool
		logger.onDebug = func() { admittedInsideLog = host.errorSurfaceMessageAllowed("") }
		noteFail(host, 3)
		if !admittedInsideLog {
			t.Fatal("benign-abort diagnostic ran before restoring visible-surface admission")
		}
	})

	t.Run("confirmed gate cancellation", func(t *testing.T) {
		logger := &debugReentrantLogger{captureLogger: &captureLogger{}}
		host := New(Config{PinNavigationToOrigin: true, Logger: logger})
		stubExternalOpen(host)
		host.noteNavigationOutcome(false, statusNone, 1)
		issueCurrentErrorSurface(t, host)
		host.noteSurfaceNavigationStarting(host.errorSurfaceURL, 2)
		noteOK(host, 2)
		cancelNavigation(host, "https://evil.example/", 7, true)

		var admittedInsideLog bool
		logger.onDebug = func() { admittedInsideLog = host.errorSurfaceMessageAllowed("") }
		host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 7)
		if !admittedInsideLog {
			t.Fatal("cancellation diagnostic ran before restoring visible-surface admission")
		}
	})
}

// The completion callback itself has a re-entrancy boundary before the error
// surface side effect: its ordinary Debug diagnostic. A failed claimed fallback
// must be sealed before that callback reaches the embedder's Logger, otherwise a
// nested empty-source message can still execute the fallback's native controls.
func TestProductionCompletionSealsFallbackBeforeDiagnosticReentrancy(t *testing.T) {
	logger := &debugReentrantLogger{captureLogger: &captureLogger{}}
	host := New(Config{StartHidden: true, Logger: logger})
	stubExternalOpen(host)
	host.noteNavigationOutcome(false, statusNone, 1)
	issueCurrentErrorSurface(t, host)
	if !host.noteSurfaceNavigationStarting(host.errorSurfaceURL, 6) {
		t.Fatal("fallback start was not claimed")
	}
	browser := host.newWebViewBrowser()

	logger.onDebug = func() { postObservedWindowClose(browser) }
	browser.NavigationCompletedCallback(webview2.NavigationCompletedObservation{
		WebErrorStatus: webview2.WebErrorStatus(1),
		NavigationID:   6,
	})

	if host.errorSurfaceActive {
		t.Fatal("failed fallback completion retained native-control admission")
	}
	if strings.Contains(logger.String(), "quit requested") {
		t.Fatal("re-entrant empty-source WindowClose reached Quit before the fallback failure was sealed")
	}
}

// The warning emitted by classification is the first arbitrary-code boundary
// in the production completion callback. Re-entering with an empty-URI start
// must not claim the merely planned generation, and its empty-source WindowClose
// must remain rejected. An unembedded Browser makes any stale outer Navigate
// deterministic: it would report "error surface navigate".
func TestProductionCompletionDoesNotExposeAClaimBeforeNavigate(t *testing.T) {
	host, logger := newReentrantSurfaceHost(t)
	browser := host.newWebViewBrowser()

	var claimedBeforeNavigate bool
	logger.onWarn = func() {
		browser.NavigationStartingCallback(webview2.NavigationStartingObservation{
			URI:          "",
			NavigationID: 77,
		})
		claimedBeforeNavigate = host.errorSurfaceActive || host.errorSurfacePending
		postObservedWindowClose(browser)
	}

	browser.NavigationCompletedCallback(webview2.NavigationCompletedObservation{
		WebErrorStatus: webview2.WebErrorStatusConnectionAborted,
		NavigationID:   42,
	})

	logText := logger.String()
	if claimedBeforeNavigate {
		t.Fatal("re-entrant empty-URI start claimed a fallback generation before Navigate was issued")
	}
	if strings.Contains(logText, "quit requested") {
		t.Fatal("pre-Navigate empty-source WindowClose reached Quit")
	}
	if strings.Contains(logText, "error surface navigate") {
		t.Fatal("outer completion called Navigate after the re-entrant start invalidated its plan")
	}
}

func TestProductionCompletionDropsStalePlanAfterNestedCompletion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		nested webview2.NavigationCompletedObservation
	}{
		{
			name: "success",
			nested: webview2.NavigationCompletedObservation{
				IsSuccess:    true,
				NavigationID: 43,
			},
		},
		{
			name: "unclassifiable",
			nested: webview2.NavigationCompletedObservation{
				IsSuccessErr: errors.New("success unavailable"),
				NavigationID: 43,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := &debugReentrantLogger{captureLogger: &captureLogger{}}
			host := New(Config{URL: testExternalURL, Logger: logger})
			stubExternalOpen(host)
			browser := host.newWebViewBrowser()
			logger.onDebug = func() {
				browser.NavigationCompletedCallback(tc.nested)
			}

			browser.NavigationCompletedCallback(webview2.NavigationCompletedObservation{
				WebErrorStatus: webview2.WebErrorStatusConnectionAborted,
				NavigationID:   42,
			})

			if host.errorSurfacePlan != noErrorSurfacePlan ||
				host.errorSurfacePending || host.errorSurfaceLoading {
				t.Fatal("nested completion left the outer fallback plan authoritative")
			}
			if strings.Contains(logger.String(), "error surface navigate") {
				t.Fatal("outer completion called Navigate after the nested completion invalidated its plan")
			}
		})
	}
}
