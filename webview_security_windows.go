//go:build windows

package mullion

import (
	"github.com/Burakuslendera/mullion/internal/logsafe"
	"github.com/Burakuslendera/mullion/internal/webview2"
)

// applyWebViewHardening reduces the browser surface a shipped app exposes.
//
// Disabled by default: DevTools, the default context menu, the browser
// accelerator keys (Ctrl+R reload, F12, Ctrl+Shift+I, Ctrl+P, Ctrl+F) and the
// status bar. The accelerators matter more than they look: Ctrl+R reloads the
// document, which in a frameless window silently resets the frontend's state
// while the native frame keeps running.
//
// Never touched: IsScriptEnabled and IsWebMessageEnabled. Turning either off
// would break the application's own frontend and the bridge it talks over.
//
// Config.DevTools re-enables the developer surface for debugging.
//
// Each setter is applied independently, and the accelerator keys live on a later
// interface revision than the rest: a runtime too old to implement it is a
// warning, not a failure, and everything else still applies.
func (host *Host) applyWebViewHardening(browser *webview2.Browser) {
	if browser == nil {
		host.log.Warn("mullion: webview hardening skipped, browser unavailable")
		return
	}
	settings, err := browser.Settings()
	if err != nil {
		host.log.Warn("mullion: webview hardening skipped, settings unavailable, reason=" + logsafe.Reason(err))
		return
	}
	enabled := host.config.DevTools

	host.warnIf("devtools setting", settings.PutAreDevToolsEnabled(enabled))
	host.warnIf("context menu setting", settings.PutAreDefaultContextMenusEnabled(enabled))
	if !enabled {
		host.warnIf("status bar disable", settings.PutIsStatusBarEnabled(false))
	}

	settings3, err := settings.QuerySettings3()
	if err != nil {
		host.log.Warn("mullion: accelerator key setting unavailable, reason=" + logsafe.Reason(err))
	} else {
		host.warnIf("accelerator keys setting", settings3.PutAreBrowserAcceleratorKeysEnabled(enabled))
		settings3.Release()
	}

	if enabled {
		host.log.Debug("mullion: webview devtools enabled")
		return
	}
	host.log.Debug("mullion: webview hardening applied")
}
