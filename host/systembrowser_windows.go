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
		host.log.Debug("mullion: new window dropped, unsupported scheme, uri=" + logsafe.Message(clampSourceForLog(uri)))
		return
	}
	host.log.Debug("mullion: new window routed to system browser, user_initiated=" +
		strconv.FormatBool(isUserInitiated) + ", uri=" + logsafe.Message(clampSourceForLog(uri)))
	host.openInSystemBrowser(uri)
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

// openInSystemBrowser hands an http/https URL to the user's default browser via
// ShellExecute. Callers gate the URL with isExternalBrowserSafe first; this does
// not re-check. It is the one piece of the new-window path a headless test
// cannot exercise - it would launch a browser - so it is verified live.
func (host *Host) openInSystemBrowser(uri string) {
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
