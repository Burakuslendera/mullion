//go:build windows

package host

import (
	"errors"
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

func claimObservedFallback(t *testing.T, host *Host, navigationID uint64) {
	t.Helper()
	host.errorSurfaceURL = "data:text/html,surface"
	noteFail(host, 0)
	issueCurrentErrorSurface(t, host)
	if !host.noteSurfaceNavigationStarting(host.errorSurfaceURL, navigationID) {
		t.Fatal("fallback start was not claimed")
	}
}

func postObservedWindowClose(browser *webview2.Browser) {
	browser.MessageCallback(webview2.WebMessageObservation{
		Message: `{"id":"close","method":"` + methodClose + `","args":[]}`,
	}, nil)
}

func TestProductionNavigationStartingDoesNotCancelExactCredentialedPlanStart(t *testing.T) {
	credentialedURL := "http://user:secret@" +
		strings.TrimPrefix(testExternalURL, "http://") + "/private?query#fragment"
	host, _ := newTestHost(t, Config{
		URL:                   credentialedURL,
		PinNavigationToOrigin: true,
	})
	if host.source.startURL != credentialedURL {
		t.Fatalf("canonical plan start = %q, want %q", host.source.startURL, credentialedURL)
	}

	// This drives the production callback with the URI observation WebView2
	// supplies. Whether a live runtime rewrites credentials or escapes remains
	// live-only evidence: stripping all userinfo safely falls back to canonical
	// origin matching, while any partial rewrite intentionally loses this grant.

	browser := host.newWebViewBrowser()
	if browser.NavigationStartingCallback(webview2.NavigationStartingObservation{
		URI:          host.source.startURL,
		NavigationID: 7,
	}) {
		t.Fatal("production callback cancelled the exact caller-authorized plan start")
	}
}

func TestProductionNavigationStartingDisarmsClaimedFallbackOnUnreadableTarget(t *testing.T) {
	host, logger := newTestHost(t, Config{PinNavigationToOrigin: true})
	claimObservedFallback(t, host, 6)
	browser := host.newWebViewBrowser()

	cancel := browser.NavigationStartingCallback(webview2.NavigationStartingObservation{
		URIErr:       errors.New("uri unavailable"),
		NavigationID: 7,
	})

	if !cancel {
		t.Fatal("an origin pin must fail closed when the target URI is unreadable")
	}
	if host.errorSurfacePending || host.errorSurfaceActive || !host.errorSurfaceSuspended {
		t.Fatalf("unreadable later start did not suspend only fallback admission: pending=%t active=%t suspended=%t",
			host.errorSurfacePending, host.errorSurfaceActive, host.errorSurfaceSuspended)
	}
	postObservedWindowClose(browser)
	if strings.Contains(logger.String(), "quit requested") {
		t.Fatal("unreadable later start retained empty-source WindowClose")
	}
	if got := strings.Count(logger.String(), "event=NavigationStarting, getter=GetUri"); got != 1 {
		t.Fatalf("URI diagnostics = %d, want exactly 1:\n%s", got, logger.String())
	}
}

func TestProductionNavigationStartingPreservesZeroIDProvenance(t *testing.T) {
	for _, tc := range []struct {
		name        string
		observation webview2.NavigationStartingObservation
		wantKnown   bool
	}{
		{
			name: "known zero",
			observation: webview2.NavigationStartingObservation{
				URI:          "https://foreign.example/zero",
				NavigationID: 0,
			},
			wantKnown: true,
		},
		{
			name: "getter failure",
			observation: webview2.NavigationStartingObservation{
				URI:             "https://foreign.example/unavailable",
				NavigationID:    0,
				NavigationIDErr: errors.New("id unavailable"),
			},
			wantKnown: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, _ := newTestHost(t, Config{PinNavigationToOrigin: true})
			claimObservedFallback(t, host, 6)
			browser := host.newWebViewBrowser()

			if !browser.NavigationStartingCallback(tc.observation) {
				t.Fatal("foreign start escaped the origin pin")
			}
			if !host.errorSurfaceSuspended {
				t.Fatal("foreign start did not suspend fallback admission")
			}
			if host.errorSurfaceDeparture.known != tc.wantKnown ||
				host.errorSurfaceDeparture.value != 0 {
				t.Fatalf("departure identity = %+v, want known=%t value=0",
					host.errorSurfaceDeparture, tc.wantKnown)
			}
		})
	}
}

func TestProductionAnonymousCancelOverlapCannotRestoreNewerSuspension(t *testing.T) {
	host, logger := newTestHost(t, Config{PinNavigationToOrigin: true})
	claimObservedFallback(t, host, 6)
	browser := host.newWebViewBrowser()

	knownZero := webview2.NavigationStartingObservation{
		URI:          "https://foreign.example/older-zero",
		NavigationID: 0,
	}
	if !browser.NavigationStartingCallback(knownZero) {
		t.Fatal("known-zero foreign start escaped the origin pin")
	}
	browser.NavigationCancelledCallback(knownZero)

	unknown := webview2.NavigationStartingObservation{
		URI:             "https://foreign.example/newer-unknown",
		NavigationIDErr: errors.New("id unavailable"),
	}
	if !browser.NavigationStartingCallback(unknown) {
		t.Fatal("unknown-id foreign start escaped the origin pin")
	}
	browser.NavigationCancelledCallback(unknown)
	if host.errorSurfaceDeparture.known {
		t.Fatal("newer unknown departure was collapsed into known zero")
	}

	// The older known-zero completion has a valid cleanup credit, but zero is
	// not unique attribution. It must not restore authority reserved for the
	// newer unknown suspension.
	browser.NavigationCompletedCallback(webview2.NavigationCompletedObservation{
		IsSuccess:      false,
		WebErrorStatus: webview2.WebErrorStatusOperationCanceled,
		NavigationID:   0,
	})
	if host.errorSurfaceActive || !host.errorSurfaceSuspended {
		t.Fatalf("older anonymous completion restored or cleared newer suspension: active=%t suspended=%t",
			host.errorSurfaceActive, host.errorSurfaceSuspended)
	}
	if host.errorSurfaceDeparture.known {
		t.Fatal("older completion replaced the newer unknown departure identity")
	}
	postObservedWindowClose(browser)
	if strings.Contains(logger.String(), "quit requested") {
		t.Fatal("older anonymous completion admitted empty-source controls")
	}

	// The newer unavailable-id completion may spend only its own cleanup
	// credit. Getter failure seals authority rather than restoring by order.
	browser.NavigationCompletedCallback(webview2.NavigationCompletedObservation{
		IsSuccess:       false,
		WebErrorStatus:  webview2.WebErrorStatusOperationCanceled,
		NavigationIDErr: errors.New("id unavailable"),
	})
	if host.errorSurfaceActive || host.errorSurfaceSuspended {
		t.Fatalf("unknown completion retained fallback authority: active=%t suspended=%t",
			host.errorSurfaceActive, host.errorSurfaceSuspended)
	}
	if host.cancelledNavAnonymous != 0 || host.cancelledNavUnknown != 0 {
		t.Fatalf("anonymous ledger not drained: total=%d unknown=%d",
			host.cancelledNavAnonymous, host.cancelledNavUnknown)
	}
}

func TestProductionNavigationStartingPinsArbitraryDataButNotClaimedFallback(t *testing.T) {
	host, logger := newTestHost(t, Config{PinNavigationToOrigin: true})
	host.errorSurfaceURL = "data:text/html,surface"
	noteFail(host, 0)
	issueCurrentErrorSurface(t, host)
	browser := host.newWebViewBrowser()

	if browser.NavigationStartingCallback(webview2.NavigationStartingObservation{
		URI:          host.errorSurfaceURL,
		NavigationID: 8,
	}) {
		t.Fatal("positively claimed exact fallback start was passed to the origin pin")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("exact fallback claim did not immediately admit its controls")
	}
	if !browser.NavigationStartingCallback(webview2.NavigationStartingObservation{
		URI:          "data:text/html,foreign",
		NavigationID: 9,
	}) {
		t.Fatal("arbitrary unclaimed data navigation escaped the origin pin")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("foreign data start retained fallback controls")
	}
	if !host.errorSurfaceSuspended {
		t.Fatal("foreign data start did not record the restorable departure")
	}
	postObservedWindowClose(browser)
	if strings.Contains(logger.String(), "quit requested") {
		t.Fatal("foreign data start retained empty-source WindowClose")
	}
}

func TestProductionForeignSuccessfulStartCannotClaimPendingFallback(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})
	host.errorSurfaceURL = "data:text/html,surface"
	if !noteFail(host, 0) {
		t.Fatal("failed document did not arm a pending fallback generation")
	}
	issueCurrentErrorSurface(t, host)
	browser := host.newWebViewBrowser()

	if browser.NavigationStartingCallback(webview2.NavigationStartingObservation{
		URI:          "https://foreign.example/app",
		NavigationID: 8,
	}) {
		t.Fatal("origin pin is disabled, but the foreign start was cancelled")
	}

	if !host.errorSurfacePending || host.errorSurfaceActive {
		t.Fatalf("foreign start changed pending fallback authority: pending=%t active=%t",
			host.errorSurfacePending, host.errorSurfaceActive)
	}
	postObservedWindowClose(browser)
	if strings.Contains(logger.String(), "quit requested") {
		t.Fatal("foreign successful URI was collapsed to an empty fallback claim")
	}
}

func TestProductionCompletionGetterFailuresSealClaimedFallback(t *testing.T) {
	for _, tc := range []struct {
		name        string
		observation webview2.NavigationCompletedObservation
		getter      string
	}{
		{
			name: "success unavailable",
			observation: webview2.NavigationCompletedObservation{
				IsSuccessErr:   errors.New("success unavailable"),
				WebErrorStatus: webview2.WebErrorStatusConnectionAborted,
				NavigationID:   7,
			},
			getter: "GetIsSuccess",
		},
		{
			name: "status unavailable on failure",
			observation: webview2.NavigationCompletedObservation{
				IsSuccess:         false,
				WebErrorStatusErr: errors.New("status unavailable"),
				NavigationID:      7,
			},
			getter: "GetWebErrorStatus",
		},
		{
			name: "id unavailable",
			observation: webview2.NavigationCompletedObservation{
				IsSuccess:       false,
				WebErrorStatus:  webview2.WebErrorStatusConnectionAborted,
				NavigationIDErr: errors.New("id unavailable"),
			},
			getter: "GetNavigationID",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, logger := newTestHost(t, Config{})
			claimObservedFallback(t, host, 7)
			host.noteAndGateNavigation("https://departure.example/", 8)
			browser := host.newWebViewBrowser()
			logStart := len(logger.String())

			browser.NavigationCompletedCallback(tc.observation)

			if host.errorSurfacePending || host.errorSurfaceLoading || host.errorSurfaceActive ||
				host.errorSurfaceSuspended {
				t.Fatalf("getter failure retained fallback state: pending=%t loading=%t active=%t suspended=%t",
					host.errorSurfacePending, host.errorSurfaceLoading, host.errorSurfaceActive,
					host.errorSurfaceSuspended)
			}
			postObservedWindowClose(browser)
			logText := logger.String()[logStart:]
			if strings.Contains(logText, "quit requested") {
				t.Fatal("getter failure retained empty-source WindowClose")
			}
			if got := strings.Count(logText, "event=NavigationCompleted, getter="+tc.getter); got != 1 {
				t.Fatalf("%s diagnostics = %d, want exactly 1:\n%s", tc.getter, got, logText)
			}
			if strings.Contains(logText, "status=0") || strings.Contains(logText, "id=0") ||
				strings.Contains(logText, "success=false") {
				t.Fatalf("getter failure produced a fabricated scalar:\n%s", logText)
			}
		})
	}
}

func TestProductionCompletionUsesKnownSuccessWhenStatusUnavailable(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	claimObservedFallback(t, host, 9)
	browser := host.newWebViewBrowser()
	logStart := len(logger.String())

	browser.NavigationCompletedCallback(webview2.NavigationCompletedObservation{
		IsSuccess:         true,
		WebErrorStatusErr: errors.New("status unavailable"),
		NavigationID:      9,
	})

	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("irrelevant status failure discarded a known successful fallback completion")
	}
	logText := logger.String()[logStart:]
	if got := strings.Count(logText, "event=NavigationCompleted, getter=GetWebErrorStatus"); got != 1 {
		t.Fatalf("status diagnostics = %d, want exactly 1:\n%s", got, logText)
	}
	if strings.Contains(logText, "status=0") {
		t.Fatalf("failed status getter produced a fabricated value:\n%s", logText)
	}
}

func TestProductionCompletionConsumesKnownCancelledIDWithoutStatus(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	claimObservedFallback(t, host, 7)
	host.rememberCancelledNavigation(9)
	browser := host.newWebViewBrowser()
	logStart := len(logger.String())

	browser.NavigationCompletedCallback(webview2.NavigationCompletedObservation{
		IsSuccess:         false,
		WebErrorStatusErr: errors.New("status unavailable"),
		NavigationID:      9,
	})

	if host.cancelledNavIDs[0] != 0 {
		t.Fatal("known cancellation id was not consumed by identity")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("unclassifiable cancelled completion retained stale fallback controls")
	}
	logText := logger.String()[logStart:]
	if strings.Contains(logText, "status=0") || strings.Contains(logText, "id=0") {
		t.Fatalf("cancel classification fabricated an unavailable scalar:\n%s", logText)
	}
}

type eventGetterReentrantLogger struct {
	*captureLogger
	onError func()
}

func (logger *eventGetterReentrantLogger) Error(message string) {
	logger.captureLogger.Error(message)
	if hook := logger.onError; hook != nil {
		logger.onError = nil
		hook()
	}
}

func TestEventGetterStateTransitionPrecedesDiagnosticReentrancy(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*webview2.Browser)
	}{
		{
			name: "starting URI",
			run: func(browser *webview2.Browser) {
				browser.NavigationStartingCallback(webview2.NavigationStartingObservation{
					URIErr: errors.New("uri unavailable"),
				})
			},
		},
		{
			name: "completed success",
			run: func(browser *webview2.Browser) {
				browser.NavigationCompletedCallback(webview2.NavigationCompletedObservation{
					IsSuccessErr: errors.New("success unavailable"),
				})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := &eventGetterReentrantLogger{captureLogger: &captureLogger{}}
			host := New(Config{PinNavigationToOrigin: true, Logger: logger})
			stubExternalOpen(host)
			claimObservedFallback(t, host, 7)
			browser := host.newWebViewBrowser()
			logger.onError = func() { postObservedWindowClose(browser) }

			tc.run(browser)

			if strings.Contains(logger.String(), "quit requested") {
				t.Fatal("getter Logger reentrancy reached stale fallback WindowClose")
			}
		})
	}
}

func TestEnsureWebViewReturnsCreationErrorWithoutReportingIt(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	wantErr := errors.New("embed failed")

	err := host.ensureWebViewWith("initial", func() error { return wantErr })

	if !errors.Is(err, wantErr) {
		t.Fatalf("ensureWebViewWith err = %v, want %v", err, wantErr)
	}
	if strings.Contains(logger.String(), wantErr.Error()) {
		t.Fatalf("returned creation error was also reported by the inner helper:\n%s", logger.String())
	}
}

func TestShowReportsReturnedCreationErrorExactlyOnce(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})
	beginHeadlessLifecycleRun(t, host, windowHandle(0x5151))
	defer host.endRun()
	wantErr := errors.New("filter failed")
	host.sendNativeCommand = func(windowHandle, uint32, uintptr, uintptr) (uintptr, error) {
		if host.showFromMessageWithEnsure(func(string) error { return wantErr }) {
			return 1, nil
		}
		return 0, nil
	}

	err := host.Show()

	if err == nil || err.Error() != "native show did not become visible" {
		t.Fatalf("Show err = %v, want public visibility failure", err)
	}
	if got := strings.Count(logger.String(), wantErr.Error()); got != 1 {
		t.Fatalf("terminal reports = %d, want exactly 1:\n%s", got, logger.String())
	}
	logText := logger.String()
	if got := strings.Count(logText, "level=ERROR") + strings.Count(logText, "level=WARN"); got != 1 {
		t.Fatalf("terminal severity lines = %d, want exactly 1:\n%s", got, logText)
	}
}
