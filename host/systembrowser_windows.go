//go:build windows

package host

import (
	"net/url"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

// SW_SHOWNORMAL: open the browser in a normal (non-minimised) window.
const swShowNormal = 1

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

// openInSystemBrowser hands an http/https URL to whatever application Windows
// associates with the URL's scheme, via ShellExecute with the "open" verb - the
// user's default browser in the normal case. It deliberately picks no browser
// and forces none: the association is the user's to make. When no default is set,
// Windows shows its own "How do you want to open this?" chooser, which is the
// right outcome - the user selects one - and ShellExecute still returns success.
// Only a scheme with no association at all fails (SE_ERR_NOASSOC = 31 <= 32),
// which the warning below records; on Windows 10/11 http/https always resolves to
// at least Edge, so that is effectively unreachable. Callers gate the URL with
// isExternalBrowserSafe first; this does not re-check. It is the one piece of the
// routing a headless test cannot exercise - it would launch a browser - so it is
// verified live.
func (host *Host) openInSystemBrowser(uri string) {
	if host.openExternal != nil {
		// The test seam (issue #76). Nothing in production sets it.
		host.openExternal(uri)
		return
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		host.log.Warn("mullion: external open skipped, reason=" + logsafe.Reason(err))
		return
	}
	target, err := windows.UTF16PtrFromString(uri)
	if err != nil {
		host.log.Warn("mullion: external open skipped, bad url, reason=" + logsafe.Reason(err))
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
		host.log.Warn("mullion: external open failed, shellexecute code=" + strconv.FormatUint(uint64(ret), 10))
	}
}
