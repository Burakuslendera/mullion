//go:build windows

package host

import (
	"strconv"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

// The two WebView2 callbacks that log a URL, lifted out of the closures in
// createWebView so the suite can drive them without a runtime.
//
// The closures themselves cannot be reached headless - createWebView needs a
// live WebView2 - but neither of these bodies touches the browser, the
// controller or any COM object. They read host.log and host.config only, which
// is exactly the shape NewWindowRequestedCallback already has: it is a one-line
// delegation to routeNewWindow, and that is precisely why routeNewWindow's log
// line is locked by a test and these two were not. Same seam, same reason.
//
// This matters beyond tidiness. Every live verification in this milestone was
// read off these lines, and before issue #78 they had already lost the host they
// appeared to name.

// logNavigationStarting records a navigation the runtime is about to begin. The
// id is what ties this line to the completion that follows it, and the uri is
// reduced with logsafe.URL because this field's value *is* a URL: URL bounds the
// whole of it and refuses to print a host it cannot print in full
// (decisions/0025). logsafe.Message would keep the host too since issue #80, but
// it bounds nothing, and a runtime-supplied URI has no length this host chose.
func (host *Host) logNavigationStarting(uri string, navigationID uint64, isUserInitiated, isRedirected bool) {
	host.log.Debug("mullion: navigation starting, id=" + formatUint64(navigationID) +
		", user_initiated=" + strconv.FormatBool(isUserInitiated) +
		", redirected=" + strconv.FormatBool(isRedirected) +
		", uri=" + logsafe.URL(uri))
}

// logRejectedWebMessage records a web message dropped because its source is not
// allowed to drive the bridge.
//
// Two lines, deliberately. The WARN carries the canonical origin, which is what
// the allow-list actually decided on; the DEBUG carries the fuller logsafe
// reduction. A value without an HTTP origin retains the established :unknown
// diagnostic while the raw line distinguishes the forms at DEBUG.
func (host *Host) logRejectedWebMessage(source string) {
	host.log.Warn("mullion: web message rejected, untrusted source, origin=" + logsafe.URL(sourceOriginSummary(source)))
	host.log.Debug("mullion: web message rejected, raw source=" + logsafe.URL(source) +
		", len=" + strconv.Itoa(len(source)))
}
