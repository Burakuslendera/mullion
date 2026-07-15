//go:build windows

package host

import (
	"errors"

	"github.com/Burakuslendera/mullion/internal/logsafe"
	"github.com/Burakuslendera/mullion/internal/webview2"
)

func (host *Host) ensureWebView(source string) error {
	if host.browser != nil {
		return nil
	}
	host.log.Debug("mullion: webview create requested, source=" + logsafe.Message(source))
	if err := host.createWebView(); err != nil {
		host.log.Error("mullion: webview2 embed failed, source=" + logsafe.Message(source) + ", reason=" + logsafe.Reason(err))
		return err
	}
	return nil
}

func (host *Host) isWebViewDeferred() bool {
	return host.config.StartHidden && host.browser == nil
}

// createWebView embeds the control and prepares it for the first navigation.
//
// The order below is a contract, not a style choice. Settings, the injected
// scripts and non-client region support must all be applied after Embed and
// before the first Navigate: WebView2 applies several of them "on the next
// navigation", so doing any of it later either has no effect on the first paint
// or forces a second navigation, which shows up as a reload flash.
func (host *Host) createWebView() error {
	host.log.Debug("mullion: webview2 instance requested")
	browser := webview2.New()
	browser.UserDataFolder = host.config.UserDataFolder
	browser.AdditionalBrowserArguments = host.config.BrowserArguments

	browser.ErrorCallback = func(err error) {
		host.log.Error("mullion: webview2 runtime error, reason=" + logsafe.Reason(err))
	}
	browser.MessageCallback = func(message string, sender *webview2.ICoreWebView2) {
		response := host.handleWebMessage(message)
		if response == "" {
			return
		}
		if sender == nil {
			host.log.Warn("mullion: bridge response sender unavailable")
			return
		}
		if err := sender.PostWebMessageAsString(response); err != nil {
			host.log.Warn("mullion: bridge response post failed, reason=" + logsafe.Reason(err))
		}
	}
	browser.WebResourceRequestedCallback = func(request *webview2.ICoreWebView2WebResourceRequest, args *webview2.ICoreWebView2WebResourceRequestedEventArgs) {
		host.assets.webResourceRequested(request, args, browser.Environment())
	}
	browser.NavigationCompletedCallback = func(success bool, status webview2.WebErrorStatus) {
		if !success {
			host.log.Warn("mullion: navigation failed, status=" + formatInt32(int32(status)))
		}
		host.log.Debug("mullion: navigation completed")
		host.syncWebViewBounds("navigation_completed")
		host.warnIf("navigation diagnostic eval", browser.Eval(host.js.navigationEval))
	}
	browser.ProcessFailedCallback = func(kind webview2.ProcessFailedKind) {
		host.log.Error("mullion: webview2 process failed, kind=" + formatInt32(int32(kind)))
	}

	host.log.Debug("mullion: webview2 embed requested")
	if err := browser.Embed(uintptr(host.window())); err != nil {
		return errors.Join(errors.New("embed webview2"), err)
	}
	host.browser = browser
	host.log.Debug("mullion: webview2 embedded")

	background := host.config.BackgroundColour
	host.warnIf("background colour", browser.SetBackgroundColour(background.R, background.G, background.B, background.A))
	host.applyWebViewHardening(browser)
	host.syncWebViewBounds("embed")

	host.log.Debug("mullion: webresource filter registered")
	host.warnIf("web resource filter", browser.AddWebResourceRequestedFilter(host.config.origin()+"/*", webview2.WebResourceContextAll))
	host.log.Debug("mullion: asset serving ready")

	// The bridge script installs the namespace the other three scripts use, so
	// it must be injected first.
	host.warnIf("bridge script", browser.Init(host.js.bridge))
	host.warnIf("diagnostics script", browser.Init(host.js.diagnostics))
	host.warnIf("drag script", browser.Init(host.js.drag))
	host.warnIf("resize script", browser.Init(host.js.resize))
	host.log.Debug("mullion: injected scripts registered")

	host.applyTabStripStartup(browser)
	host.log.Debug("mullion: navigate requested")
	host.startRenderWatchdog()
	return browser.Navigate(host.config.startURL())
}
