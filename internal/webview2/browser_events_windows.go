//go:build windows

package webview2

// Event registration: the six COM events the Browser turns into Go callbacks,
// and the reference discipline each one needs - the handler dropped as soon as
// add_* has taken its own, the WebResourceRequested request dropped after the
// host callback returns. Split from browser_windows.go, which keeps the
// lifecycle that creates the core these handlers are registered on.

import (
	"errors"
	"unsafe"
)

// addEvent registers one handler and immediately drops the package's reference
// to it.
//
// add_* takes its own reference, so the object survives this release and lives
// until the WebView is torn down. Holding on to our reference as well would leak
// one COM object per handler; releasing before add_* would hand the runtime a
// freed object.
func addEvent(handler unsafe.Pointer, register func(unsafe.Pointer) (EventRegistrationToken, error)) error {
	_, err := register(handler)
	ReleaseHandler(handler)
	return err
}

func (browser *Browser) registerEvents() error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: core webview unavailable")
	}

	if err := addEvent(NewWebMessageReceivedHandler(func(sender *ICoreWebView2, args *ICoreWebView2WebMessageReceivedEventArgs) {
		if browser.MessageCallback == nil {
			return
		}
		message, err := args.TryGetWebMessageAsString()
		if err != nil {
			// A frontend may post a structured message rather than a string.
			// The bridge protocol here is string-based, so dropping it is
			// correct - there is nothing the host could do with it.
			return
		}
		// GetSource is the URI of the document that posted the message; the host
		// uses it to keep the bridge pinned to the trusted origin.
		source, _ := args.GetSource()
		browser.MessageCallback(message, source, sender)
	}), core.AddWebMessageReceived); err != nil {
		return err
	}

	if err := addEvent(NewWebResourceRequestedHandler(func(_ *ICoreWebView2, args *ICoreWebView2WebResourceRequestedEventArgs) {
		browser.handleWebResourceRequested(args)
	}), core.AddWebResourceRequested); err != nil {
		return err
	}

	if err := addEvent(NewNavigationStartingHandler(func(_ *ICoreWebView2, args *ICoreWebView2NavigationStartingEventArgs) {
		if browser.NavigationStartingCallback == nil {
			return
		}
		// The getters degrade independently on purpose: identity is the
		// load-bearing value, and a URI read failing must not cost the id. A
		// failed id read reports 0, which no real navigation uses - callers
		// treat it as "identity unavailable" (decisions/0021).
		//
		// Both failures are reported. An unreadable URI is not cosmetic here:
		// a host gate that decides on the target sees the empty string, which
		// is no origin's, so it decides against a navigation it could not read
		// (issue #73). That has to be diagnosable, and it was silent.
		uri, err := args.GetUri()
		if err != nil {
			browser.reportWarning(err)
		}
		id, err := args.GetNavigationID()
		if err != nil {
			browser.reportWarning(err)
		}
		userInitiated, _ := args.GetIsUserInitiated()
		redirected, _ := args.GetIsRedirected()
		if !browser.NavigationStartingCallback(uri, id, userInitiated, redirected) {
			return
		}
		// Cancel first, tell the host second. A failed put_Cancel means the
		// navigation is still going ahead, so the host must not act on a
		// cancel that did not happen - no id to swallow the completion with,
		// and no second copy of the target opened somewhere else (issue #73).
		if err := args.PutCancel(true); err != nil {
			browser.reportWarning(err)
			return
		}
		if browser.NavigationCancelledCallback != nil {
			browser.NavigationCancelledCallback(uri, id, userInitiated)
		}
	}), core.AddNavigationStarting); err != nil {
		return err
	}

	if err := addEvent(NewNavigationCompletedHandler(func(_ *ICoreWebView2, args *ICoreWebView2NavigationCompletedEventArgs) {
		if browser.NavigationCompletedCallback == nil {
			return
		}
		success, _ := args.GetIsSuccess()
		status, _ := args.GetWebErrorStatus()
		id, err := args.GetNavigationID()
		if err != nil {
			browser.reportWarning(err)
		}
		browser.NavigationCompletedCallback(success, status, id)
	}), core.AddNavigationCompleted); err != nil {
		return err
	}

	if err := addEvent(NewNewWindowRequestedHandler(func(_ *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs) {
		// Suppress the runtime's default new window first - a detached
		// CoreWebView2 with no host chrome is meaningless for a single-window
		// frameless host (issue #6). If suppression fails (a broken args object),
		// the runtime opens its own window, so do not also route the URI to the
		// system browser: that would double-open. Otherwise the suppression stands
		// even when the callback is unset or the URI read fails.
		if err := args.PutHandled(true); err != nil {
			browser.reportWarning(err)
			return
		}
		if browser.NewWindowRequestedCallback == nil {
			return
		}
		uri, _ := args.GetUri()
		userInitiated, _ := args.GetIsUserInitiated()
		browser.NewWindowRequestedCallback(uri, userInitiated)
	}), core.AddNewWindowRequested); err != nil {
		return err
	}

	return addEvent(NewProcessFailedHandler(func(_ *ICoreWebView2, args *ICoreWebView2ProcessFailedEventArgs) {
		if browser.ProcessFailedCallback == nil {
			return
		}
		kind, _ := args.GetProcessFailedKind()
		browser.ProcessFailedCallback(kind)
	}), core.AddProcessFailed)
}

// handleWebResourceRequested resolves the request out of the event args, hands
// it to the host callback, and releases it when the callback returns.
//
// GetRequest returns a reference this package owns (interfaces_webresource_windows.go),
// and this is the only code that can drop it: ICoreWebView2WebResourceRequest
// exposes no exported Release, so the host-side callback could not release the
// request even if it knew it had to. Without the release here, one COM object
// leaks per intercepted request - every document, stylesheet, script, image and
// fetch in embedded-FS mode - and grows without bound for the life of the
// window. The deferred release also runs when the callback panics, so a
// recovered handler panic (handlers_windows.go) does not turn into a leak.
//
// The args pointer is borrowed for the duration of the event and is left
// untouched (see the ownership note in handlers_windows.go); only the
// GetRequest result is owned here.
func (browser *Browser) handleWebResourceRequested(args *ICoreWebView2WebResourceRequestedEventArgs) {
	if browser.WebResourceRequestedCallback == nil || args == nil {
		return
	}
	request, err := args.GetRequest()
	if err != nil {
		browser.reportError(err)
		return
	}
	defer asUnknown(request).Release()
	browser.WebResourceRequestedCallback(request, args)
}
