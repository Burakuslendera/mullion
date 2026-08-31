//go:build windows

package host

// Tab-strip activation.
//
// createWebView calls applyTabStripStartup after Embed and before the first
// Navigate: it turns on WebView2 non-client region support and, only if that
// succeeded, injects the flag that tells the frontend the title bar may live in
// the page (CSS "app-region: drag" then produces a real HTCAPTION).
//
// The ordering is a bug fix, not a preference. Injecting the flag first and
// enabling afterwards would, on a runtime without non-client region support,
// leave the frontend in tab-strip mode with no working drag path at all: the
// page would suppress its fallback title bar while the shell still owned the
// caption. Enable first, flag second, and an old runtime simply keeps the
// JavaScript drag fallback.
//
// Support is detected by asking the settings object for ICoreWebView2Settings9,
// never by comparing runtime versions. This package loads the runtime's client
// DLL directly rather than going through the SDK loader, and the loader is where
// the minimum-version gate lives - so a version number here would prove nothing,
// while QueryInterface answers the actual question.

import (
	"errors"

	"github.com/Burakuslendera/mullion/internal/logsafe"
	"github.com/Burakuslendera/mullion/internal/webview2"
)

// applyTabStripStartup runs between the required script barrier and the first
// Navigate. Capability failures remain advisory; a destroyed/replaced Browser
// is lifecycle failure and must keep later readiness and navigation closed.
func (host *Host) applyTabStripStartup(browser *webview2.Browser) error {
	if err := host.requireCurrentBrowser(browser); err != nil {
		return err
	}
	settings, err := browser.Settings()
	if err != nil {
		return host.warnTabStripCapability(browser, "mullion: tab strip disabled, settings unavailable, reason=", err)
	}
	// Settings returns an owned reference; held for this configuration pass only.
	defer settings.Release()
	host.disableChromiumZoom(settings)

	settings9, err := settings.QuerySettings9()
	if err != nil {
		return host.warnTabStripCapability(browser, "mullion: tab strip disabled, fallback=classic_titlebar, reason=", err)
	}
	defer settings9.Release()

	if err := settings9.PutIsNonClientRegionSupportEnabled(true); err != nil {
		return host.warnTabStripCapability(browser, "mullion: tab strip disabled, fallback=classic_titlebar, reason=", err)
	}
	if err := browser.Init(host.js.tabFlag); err != nil {
		if lifecycleErr := host.requireCurrentBrowser(browser); lifecycleErr != nil {
			return lifecycleErr
		}
		host.warnIf("tab strip flag", err)
		return nil
	}
	if err := host.requireCurrentBrowser(browser); err != nil {
		return err
	}
	host.log.Debug("mullion: tab strip startup registration requested, effect=first_navigation")
	return nil
}

func (host *Host) requireCurrentBrowser(browser *webview2.Browser) error {
	if browser == nil || host.windowDestroyed || host.browser != browser || browser.IsShuttingDown() {
		return errors.New("window destroyed during webview startup")
	}
	return nil
}

func (host *Host) warnTabStripCapability(browser *webview2.Browser, prefix string, err error) error {
	if lifecycleErr := host.requireCurrentBrowser(browser); lifecycleErr != nil {
		return lifecycleErr
	}
	host.log.Warn(prefix + logsafe.Reason(err))
	return nil
}

// disableChromiumZoom removes every user zoom path (Ctrl+scroll, Ctrl+-, pinch).
//
// Zoom has to go because the frame is split between two coordinate systems: the
// resize zones and the title bar are laid out in CSS pixels, while the native
// hit test bands are computed in logical pixels scaled by window DPI. A user
// zoom scales the first and not the second, so the caption band and the drag
// region silently stop lining up with what the user sees.
//
// Pinch zoom lives on a later interface revision than zoom control, so an old
// runtime may implement one and not the other. That is a warning, not a failure:
// removing the reachable paths is still worth doing.
func (host *Host) disableChromiumZoom(settings *webview2.ICoreWebView2Settings) {
	host.warnIf("zoom control disable", settings.PutIsZoomControlEnabled(false))

	settings5, err := settings.QuerySettings5()
	if err != nil {
		host.log.Warn("mullion: pinch zoom disable skipped, reason=" + logsafe.Reason(err))
		return
	}
	defer settings5.Release()

	host.warnIf("pinch zoom disable", settings5.PutIsPinchZoomEnabled(false))
	host.log.Debug("mullion: chromium zoom disabled")
}
