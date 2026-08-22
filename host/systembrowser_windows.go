//go:build windows

package host

import (
	"errors"
	"net/url"
	"runtime"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

// SW_SHOWNORMAL: open the browser in a normal (non-minimised) window.
const swShowNormal = 1

// externalOpenLimit is how many system-browser launch workers may be in flight
// at once. It bounds concurrent goroutines, pinned OS threads, and blocking
// ShellExecuteW calls only. It is not a lifetime, per-document, per-origin, or
// time-window rate limit: once a worker returns, the same document may claim the
// slot again. Reaching the bound is reported and that launch is dropped
// (decision 0043).
//
// Eight is a shape rather than a throughput measurement, the same way
// cancelledNavSlots' is. Every launch is content-driven - a window.open or link
// activation - while a scheme handler can remain blocked beyond host control.
// Eight leaves room for ordinary repeated opens without permitting a hostile
// document to create an unbounded number of simultaneous workers.
const externalOpenLimit = 8

// routeNewWindow is called only after PutHandled(true), GetUri, and
// GetIsUserInitiated all succeed in the WebView2 adapter. PutHandled
// failure may leave runtime popup behavior in effect; either getter failure
// leaves the popup suppressed but produces no host launch. Given all three
// successes, this default-on route hands the successfully observed and admitted
// HTTP(S) URI unchanged to the system-browser activation and drops targets
// rejected by the shared gate (decisions/0022 and 0043).
//
// isUserInitiated is WebView2's diagnostic classification. It is logged but
// never gates routing and is not treated as physical-input authority.
func (host *Host) routeNewWindow(uri string, isUserInitiated bool) {
	if !isExternalBrowserSafe(uri) {
		host.log.Debug("mullion: new window dropped, target not admitted, uri=" + logsafe.Field(logsafe.URL(uri)))
		return
	}
	host.log.Debug("mullion: new window routed to system browser, user_initiated=" +
		strconv.FormatBool(isUserInitiated) + ", uri=" + logsafe.Field(logsafe.URL(uri)))
	host.openInSystemBrowser(uri)
}

// shouldCancelNavigation is the PinNavigationToOrigin gate applied to a
// NavigationStarting event, and it is a decision and nothing else: no state
// written, nothing routed, nothing logged. Everything following the request runs
// only after PutCancel accepts it (issue #73, decisions/0027 and 0037); that
// acceptance is not proof the navigation was abandoned. The containment matches
// NewWindowRequested (issue #6, decisions/0023). With the gate off (the default),
// sourcePlan.navigationOffOrigin is false for every uri.
func (host *Host) shouldCancelNavigation(uri string) bool {
	return host.source.navigationOffOrigin(uri, host.config.PinNavigationToOrigin)
}

// noteNavigationCancelled records a cancel request accepted by PutCancel. It
// enters the navigation in the cancelled ledger so an expected failed/cancelled
// completion is cleanup, and hands a successfully observed and admitted HTTP(S)
// URI unchanged to the same system-browser activation as the default-on
// new-window route. Targets rejected by the shared gate are dropped (decisions/0027
// and 0043).
//
// PutCancel success accepts the request but does not prove abandonment. A later
// successful completion means the navigation committed anyway and returns to
// ordinary policy. Keeping this callback separate still prevents a failed
// PutCancel from routing the target or swallowing the foreign document's
// completion (issue #73, decisions/0027 and 0037).
//
// An unreadable URI arrives as the empty string, which is no origin's, so the
// gate has just cancelled a navigation it could not read. That fail-closed drop
// is deliberate - a gate that lets through what it cannot identify is not a
// gate - and it is warned alongside the WebView2 getter failure. Nothing is
// routed because no successfully observed URI exists.
func (host *Host) noteNavigationCancelled(uri string, navigationID uint64, isUserInitiated bool) {
	host.noteNavigationCancelledObserved(
		uri,
		knownNavigationIdentity(navigationID),
		isUserInitiated,
	)
}

func (host *Host) noteNavigationCancelledObserved(
	uri string,
	identity navigationIdentity,
	isUserInitiated bool,
) {
	host.rememberCancelledNavigationObserved(identity)
	switch {
	case uri == "":
		host.log.Warn("mullion: navigation cancelled off origin, target unreadable, " +
			navigationIdentityField(identity))
	case isExternalBrowserSafe(uri):
		host.log.Debug("mullion: navigation cancelled off origin, routed to system browser, user_initiated=" +
			strconv.FormatBool(isUserInitiated) + ", uri=" + logsafe.Field(logsafe.URL(uri)))
		host.openInSystemBrowser(uri)
	default:
		host.log.Debug("mullion: navigation cancelled off origin, target not admitted, uri=" +
			logsafe.Field(logsafe.URL(uri)))
	}
}

// isExternalBrowserSafe reports whether uri may be handed unchanged to the
// system browser. Before parsing, literal double quotes, ASCII spaces, and C0,
// DEL, or C1 controls are refused at the ShellExecute argument boundary. This
// is an argument-boundary rejection, not URI normalization or proof of browser
// behavior. Only HTTP and HTTPS are allowed: ShellExecute launches the
// registered handler, so foreign content must not be able to name file: or an
// arbitrary custom protocol. This gate deliberately does not rewrite userinfo,
// query, or fragment; diagnostics redact them separately (decision 0043).
// url.Parse normalises scheme case, and an unparseable URL is refused.
func isExternalBrowserSafe(uri string) bool {
	for _, r := range uri {
		if r == '"' || r == ' ' || logsafe.IsControl(r) {
			return false
		}
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "http", "https":
		return true
	default:
		return false
	}
}

// openInSystemBrowser hands an HTTP(S) URI unchanged to the registered handler
// as a fresh ShellExecuteW "open" activation, off the UI thread (decision 0043).
// It does not preserve or replay an HTTP method, body, headers, referrer, opener,
// WebView profile, selected browser profile, WebView-held cookies or stored
// credentials, or session. Userinfo, query, and fragment remain part of the
// unchanged URI; Windows and the handler decide their treatment plus process,
// window/tab, and profile.
//
// Both routes enter here from a WebView2 event handler on the UI thread.
// ShellExecuteW may block until the association is resolved and the target
// application starts; keeping that work in the handler would stop the message
// loop from answering drag, caption, resize, or WebView2. The launch therefore
// gets its own worker and the handler returns immediately (issue #74,
// decision 0029). Callers gate the URI with isExternalBrowserSafe first; this
// function does not re-check or mutate it.
func (host *Host) openInSystemBrowser(uri string) {
	if host.openExternal != nil {
		// The test seam (issue #76). Nothing in production sets it, and it stays
		// synchronous deliberately: a suite that had to wait for a goroutine to
		// see where a URL went would be asserting on a race instead of on a
		// routing decision. The cost is that the bound below is not on the path
		// any routing test drives - claimExternalOpenSlot is tested directly for
		// that reason.
		host.openExternal(uri)
		return
	}
	if !host.claimExternalOpenSlot(uri) {
		return
	}
	admission := host.currentRun()
	go func() {
		defer host.releaseExternalOpenSlot()
		host.shellExecuteOpen(uri, admission)
	}()
}

// claimExternalOpenSlot takes one of the in-flight launch slots and reports
// whether it got one. It is the only place the bound is visible, so it is where
// exceeding it is said out loud: the drop is a click of the user's that will
// never happen, and nothing downstream could name it.
func (host *Host) claimExternalOpenSlot(uri string) bool {
	select {
	case host.externalOpenSlots <- struct{}{}:
		return true
	default:
		host.log.Warn("mullion: external open dropped, " + strconv.Itoa(externalOpenLimit) +
			" launches already in flight, uri=" + logsafe.Field(logsafe.URL(uri)))
		return false
	}
}

func (host *Host) releaseExternalOpenSlot() {
	<-host.externalOpenSlots
}

// shellExecuteOpen is the URI-only OS activation: ShellExecuteW with the "open"
// verb and the exact admitted URI as lpFile. It deliberately picks no browser,
// process, window, tab, profile, or session. The registered handler owns all of
// those choices and the resulting request behavior; a successful return proves
// only that the shell accepted the activation, not that any HTTP request
// preserved WebView state (decision 0043).
//
// It runs on its own worker and is the one piece of routing a headless test
// cannot exercise without launching the registered browser, so it is verified
// live. Its captured Run admission lets warnings reach the embedder's
// concurrent-safe Logger only while that Run still owns them.
func (host *Host) shellExecuteOpen(uri string, admission runAdmission) {
	// A COM apartment is thread-affine and a fresh goroutine is in none, while the
	// Go runtime may move it between OS threads at a suspension point. Pin the
	// thread before entering the STA. When entry succeeds, the CoUninitialize
	// defer is registered after the UnlockOSThread defer, so LIFO teardown
	// balances the apartment before unpinning; only then may Go reuse the thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// S_FALSE - this thread was already in a compatible apartment - arrives as
	// ERROR_INVALID_FUNCTION and still owes a CoUninitialize, which is why the
	// balance is claimed for it too. initializeCOM makes the same distinction for
	// the UI thread; this one carries no debug line, because it would run once per
	// launch rather than once per process.
	err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	if err == nil || errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		defer windows.CoUninitialize()
	} else {
		// Not fatal, and not a reason to drop the click: ShellExecuteW resolves
		// most associations without activating a COM handler at all, so refusing
		// to launch here would lose a navigation over a condition that may not
		// affect it. It is still news - nothing else in the process would say the
		// apartment was refused on a worker.
		host.warnForRun(admission, "mullion: external open apartment unavailable, reason="+logsafe.Reason(err))
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		host.warnForRun(admission, "mullion: external open skipped, reason="+logsafe.Reason(err))
		return
	}
	target, err := windows.UTF16PtrFromString(uri)
	if err != nil {
		host.warnForRun(admission, "mullion: external open skipped, bad url, reason="+logsafe.Reason(err))
		return
	}
	// ShellExecuteW returns an HINSTANCE-sized value; > 32 is success, anything
	// else is a documented error code. GetLastError is not meaningful for it, so
	// the return value is the only signal worth logging.
	ret, _, _ := procShellExecute.Call(
		0, // no owner window
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)),
		0, // no parameters
		0, // no working directory
		swShowNormal,
	)
	if ret <= 32 {
		host.warnForRun(admission, "mullion: external open failed, shellexecute code="+strconv.FormatUint(uint64(ret), 10))
	}
}

func (host *Host) warnForRun(admission runAdmission, message string) {
	if !host.enterOriginatingRun(admission) {
		return
	}
	defer host.leaveRun()
	host.log.Warn(message)
}
