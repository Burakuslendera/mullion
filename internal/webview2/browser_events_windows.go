//go:build windows

package webview2

// Event registration: the six COM events the Browser turns into Go callbacks,
// and the reference discipline each one needs - the handler dropped as soon as
// add_* has taken its own, the WebResourceRequested request dropped after the
// host callback returns. Split from browser_windows.go, which keeps the
// lifecycle that creates the core these handlers are registered on.

import (
	"errors"
	"strconv"
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
	// Registration can fail or panic; the constructor reference must be dropped
	// on both exits, while add_* retains the runtime's reference.
	defer ReleaseHandler(handler)
	_, err := register(handler)
	return err
}

func (browser *Browser) registerEvents() error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: core webview unavailable")
	}

	if err := addEvent(NewWebMessageReceivedHandler(browser.handleWebMessageReceived), core.AddWebMessageReceived); err != nil {
		return err
	}
	if err := addEvent(NewWebResourceRequestedHandler(func(_ *ICoreWebView2, args *ICoreWebView2WebResourceRequestedEventArgs) {
		browser.handleWebResourceRequested(args)
	}), core.AddWebResourceRequested); err != nil {
		return err
	}
	if err := addEvent(NewNavigationStartingHandler(func(_ *ICoreWebView2, args *ICoreWebView2NavigationStartingEventArgs) {
		browser.handleNavigationStarting(args)
	}), core.AddNavigationStarting); err != nil {
		return err
	}
	if err := addEvent(NewNavigationCompletedHandler(func(_ *ICoreWebView2, args *ICoreWebView2NavigationCompletedEventArgs) {
		browser.handleNavigationCompleted(args)
	}), core.AddNavigationCompleted); err != nil {
		return err
	}
	if err := addEvent(NewNewWindowRequestedHandler(func(_ *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs) {
		browser.handleNewWindowRequested(args)
	}), core.AddNewWindowRequested); err != nil {
		return err
	}
	return addEvent(NewProcessFailedHandler(func(_ *ICoreWebView2, args *ICoreWebView2ProcessFailedEventArgs) {
		browser.handleProcessFailed(args)
	}), core.AddProcessFailed)
}

func eventGetterError(event, getter string, err error) error {
	return errors.Join(errors.New(event+"."+getter), err)
}

func isInvalidArgument(err error) bool {
	var status HResultError
	return errors.As(err, &status) && status.HResult() == 0x80070057
}

func (browser *Browser) handleWebMessageReceived(sender *ICoreWebView2, args *ICoreWebView2WebMessageReceivedEventArgs) {
	if browser.MessageCallback == nil || args == nil {
		return
	}
	message, err := args.TryGetWebMessageAsString()
	if err != nil {
		if !isInvalidArgument(err) {
			browser.reportError(eventGetterError("WebMessageReceived", "TryGetWebMessageAsString", err))
		}
		return
	}
	source, sourceErr := args.GetSource()
	browser.MessageCallback(WebMessageObservation{
		Message:   message,
		Source:    source,
		SourceErr: sourceErr,
	}, sender)
}

func (browser *Browser) handleNavigationStarting(args *ICoreWebView2NavigationStartingEventArgs) {
	if browser.NavigationStartingCallback == nil || args == nil {
		return
	}
	uri, uriErr := args.GetUri()
	id, idErr := args.GetNavigationID()
	userInitiated, userInitiatedErr := args.GetIsUserInitiated()
	redirected, redirectedErr := args.GetIsRedirected()
	observation := NavigationStartingObservation{
		URI:                uri,
		URIErr:             uriErr,
		NavigationID:       id,
		NavigationIDErr:    idErr,
		IsUserInitiated:    userInitiated,
		IsUserInitiatedErr: userInitiatedErr,
		IsRedirected:       redirected,
		IsRedirectedErr:    redirectedErr,
	}
	if !browser.NavigationStartingCallback(observation) {
		return
	}
	// Cancel first, tell the host second. The args and every value derived from
	// it remain borrowed only for this synchronous invocation.
	if err := args.PutCancel(true); err != nil {
		browser.reportWarning(errors.Join(
			errors.New("NavigationStarting.PutCancel "+navigationIDField(id, idErr)),
			err,
		))
		return
	}
	if browser.NavigationCancelledCallback != nil {
		browser.NavigationCancelledCallback(observation)
	}
}

func navigationIDField(id uint64, getterErr error) string {
	if getterErr != nil {
		return "id=unavailable"
	}
	return "id=" + strconv.FormatUint(id, 10)
}

func (browser *Browser) handleNavigationCompleted(args *ICoreWebView2NavigationCompletedEventArgs) {
	if browser.NavigationCompletedCallback == nil || args == nil {
		return
	}
	success, successErr := args.GetIsSuccess()
	status, statusErr := args.GetWebErrorStatus()
	id, idErr := args.GetNavigationID()
	browser.NavigationCompletedCallback(NavigationCompletedObservation{
		IsSuccess:         success,
		IsSuccessErr:      successErr,
		WebErrorStatus:    status,
		WebErrorStatusErr: statusErr,
		NavigationID:      id,
		NavigationIDErr:   idErr,
	})
}

func (browser *Browser) handleNewWindowRequested(args *ICoreWebView2NewWindowRequestedEventArgs) {
	if args == nil {
		return
	}
	// Suppress the runtime first. A failure means it may open a window itself,
	// so routing as well would double-open.
	if err := args.PutHandled(true); err != nil {
		browser.reportWarning(eventGetterError("NewWindowRequested", "PutHandled", err))
		return
	}
	if browser.NewWindowRequestedCallback == nil {
		return
	}
	uri, uriErr := args.GetUri()
	userInitiated, userInitiatedErr := args.GetIsUserInitiated()
	browser.NewWindowRequestedCallback(NewWindowRequestedObservation{
		URI:                uri,
		URIErr:             uriErr,
		IsUserInitiated:    userInitiated,
		IsUserInitiatedErr: userInitiatedErr,
	})
}

func (browser *Browser) handleProcessFailed(args *ICoreWebView2ProcessFailedEventArgs) {
	if browser.ProcessFailedCallback == nil || args == nil {
		return
	}
	kind, err := args.GetProcessFailedKind()
	browser.ProcessFailedCallback(ProcessFailedObservation{Kind: kind, KindErr: err})
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
		browser.reportError(eventGetterError("WebResourceRequested", "GetRequest", err))
		return
	}
	defer asUnknown(request).Release()
	browser.WebResourceRequestedCallback(request, args)
}
