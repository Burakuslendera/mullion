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

// externalOpenLimit is how many system-browser launches may be in flight at
// once. It is a bound, not a capacity estimate: reaching it is reported, and the
// launch that reached it is dropped.
//
// Eight, and the reason is a shape rather than a measurement, the same way
// cancelledNavSlots' is. Every launch is content-driven - a window.open, a link
// click - so the number of them is chosen by the page, not by the host, and an
// unbounded one is a goroutine and an OS thread per event with a hostile
// document for a pump (the structure decisions/0027 refused for the ledger). The
// bound has to sit above what a person can produce, because dropping a click is
// a real cost: a cold browser start is seconds, and a few impatient clicks
// inside that window are ordinary. Eight is room for those with the launches
// still bounded.
const externalOpenLimit = 8

// routeNewWindow sends a new-window request - a window.open or a target=_blank
// link the runtime raised through NewWindowRequested - to the system browser
// when the target is a safe external URL, and drops it otherwise (issue #6).
//
// The runtime's own new window was already suppressed in the webview2 layer, so
// a single-window frameless host never spawns a detached, chrome-less WebView2.
// Dropping an unsafe scheme therefore means the request simply does nothing,
// which is the safe outcome: the alternative is launching whatever handler the
// scheme names, and off-origin content must not be able to reach one.
func (host *Host) routeNewWindow(uri string, isUserInitiated bool) {
	if !isExternalBrowserSafe(uri) {
		host.log.Debug("mullion: new window dropped, unsupported scheme, uri=" + logsafe.URL(uri))
		return
	}
	host.log.Debug("mullion: new window routed to system browser, user_initiated=" +
		strconv.FormatBool(isUserInitiated) + ", uri=" + logsafe.URL(uri))
	host.openInSystemBrowser(uri)
}

// shouldCancelNavigation is the PinNavigationToOrigin gate applied to a
// NavigationStarting event, and it is a decision and nothing else: no state
// written, nothing routed, nothing logged. Everything that follows a cancel is
// noteNavigationCancelled's, and runs only once the runtime has confirmed the
// cancel (issue #73, decisions/0027). What it decides is the same containment as
// NewWindowRequested (issue #6, decisions/0023). With the gate off (the default)
// navigationOffOrigin is false for every uri, so this returns false and cancels
// nothing.
func (host *Host) shouldCancelNavigation(uri string) bool {
	return host.config.navigationOffOrigin(uri)
}

// noteNavigationCancelled commits to a cancel the runtime has confirmed: it
// enters the navigation in the cancelled ledger, so its completion is read as
// cleanup rather than as a load failure, and hands an http/https target to the
// system browser - any other scheme is dropped, the same containment and routing
// as NewWindowRequested.
//
// Nothing here may run for a navigation that was not actually abandoned, which
// is why it is a separate callback rather than the tail of the decision above
// (issue #73, decisions/0027). Committing early meant a failed put_Cancel loaded
// the foreign document *and* opened it in the browser *and* swallowed its
// completion.
//
// An unreadable URI arrives as the empty string, which is no origin's, so the
// gate has just cancelled a navigation it could not read. That is the
// fail-closed direction and it is deliberate - a gate that lets through what it
// cannot identify is not a gate - but it may have killed a legitimate in-origin
// navigation, and nothing downstream can tell. It is the one drop worth a
// warning; the webview2 layer reports the underlying getter failure alongside it.
func (host *Host) noteNavigationCancelled(uri string, navigationID uint64, isUserInitiated bool) {
	host.rememberCancelledNavigation(navigationID)
	switch {
	case uri == "":
		host.log.Warn("mullion: navigation cancelled off origin, target unreadable, id=" +
			formatUint64(navigationID))
	case isExternalBrowserSafe(uri):
		host.log.Debug("mullion: navigation cancelled off origin, routed to system browser, user_initiated=" +
			strconv.FormatBool(isUserInitiated) + ", uri=" + logsafe.URL(uri))
		host.openInSystemBrowser(uri)
	default:
		host.log.Debug("mullion: navigation cancelled off origin, unsupported scheme, uri=" +
			logsafe.URL(uri))
	}
}

// isExternalBrowserSafe reports whether uri may be handed to the system browser.
// Only http and https are allowed: ShellExecute launches whatever handler is
// registered for a scheme, so a foreign document's window.open must not be able
// to name file: or an arbitrary custom protocol and have the host open it.
// url.Parse lower-cases the scheme, so the match is already case-insensitive; a
// URL that does not parse is refused.
func isExternalBrowserSafe(uri string) bool {
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

// openInSystemBrowser hands an http/https URL to the user's default browser, off
// the UI thread.
//
// Both routes into here run from a WebView2 event handler, and those run on the
// UI thread inside the message loop. ShellExecuteW blocks until the scheme
// association is resolved and the target application has started, which on a cold
// default browser is not instant - and while it blocks the loop does not pump, so
// the frameless window stops answering: no drag, no caption buttons, no resize,
// and the runtime is still waiting on the handler as well (issue #74,
// decisions/0029). The launch therefore gets a goroutine of its own, and the
// handler returns immediately.
//
// Callers gate the URL with isExternalBrowserSafe first; this does not re-check.
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
			" launches already in flight, uri=" + logsafe.URL(uri))
		return false
	}
}

func (host *Host) releaseExternalOpenSlot() {
	<-host.externalOpenSlots
}

// shellExecuteOpen is the launch itself: ShellExecute with the "open" verb,
// against whatever application Windows associates with the URL's scheme. It
// deliberately picks no browser and forces none - the association is the user's
// to make. When no default is set, Windows shows its own "How do you want to open
// this?" chooser, which is the right outcome, and ShellExecute still returns
// success. Only a scheme with no association at all fails (SE_ERR_NOASSOC = 31 <=
// 32), which the warning below records; on Windows 10/11 http/https always
// resolves to at least Edge, so that is effectively unreachable.
//
// It runs on a goroutine of its own and is the one piece of the routing a
// headless test cannot exercise - it would launch a browser - so it is verified
// live. Its captured Run admission lets warnings reach the embedder's
// concurrent-safe Logger only while that Run still owns them.
func (host *Host) shellExecuteOpen(uri string, admission runAdmission) {
	// A COM apartment is thread-affine and a fresh goroutine is in none, while the
	// Go runtime may move it between OS threads at any suspension point.
	// ShellExecuteW can activate a COM handler, so the thread is pinned and an STA
	// entered the way Run does for the UI thread. The goroutine returns right
	// after, and the runtime retires the locked thread with it, so no apartment
	// outlives its launch.
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
