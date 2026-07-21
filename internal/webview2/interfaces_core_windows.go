//go:build windows

package webview2

// ICoreWebView2 itself. Split from interfaces_windows.go, whose header carries
// the ABI contract that governs every vtable struct here.

import (
	"unsafe"
)

// ---------------------------------------------------------------------------
// ICoreWebView2  {76eceacb-0462-4d94-ac83-423a6793775e}  61 slots
//
// The long tail of add_/remove_ pairs is what makes this vtable dangerous:
// AddWebResourceRequestedFilter sits at slot 57, behind 30-odd event slots this
// package never touches. Drop one and the filter call lands on
// remove_WebResourceRequested.
// ---------------------------------------------------------------------------

type ICoreWebView2Vtbl struct {
	IUnknownVtbl
	GetSettings                            ComProc
	GetSource                              ComProc
	Navigate                               ComProc
	NavigateToString                       ComProc
	AddNavigationStarting                  ComProc
	RemoveNavigationStarting               ComProc
	AddContentLoading                      ComProc
	RemoveContentLoading                   ComProc
	AddSourceChanged                       ComProc
	RemoveSourceChanged                    ComProc
	AddHistoryChanged                      ComProc
	RemoveHistoryChanged                   ComProc
	AddNavigationCompleted                 ComProc
	RemoveNavigationCompleted              ComProc
	AddFrameNavigationStarting             ComProc
	RemoveFrameNavigationStarting          ComProc
	AddFrameNavigationCompleted            ComProc
	RemoveFrameNavigationCompleted         ComProc
	AddScriptDialogOpening                 ComProc
	RemoveScriptDialogOpening              ComProc
	AddPermissionRequested                 ComProc
	RemovePermissionRequested              ComProc
	AddProcessFailed                       ComProc
	RemoveProcessFailed                    ComProc
	AddScriptToExecuteOnDocumentCreated    ComProc
	RemoveScriptToExecuteOnDocumentCreated ComProc
	ExecuteScript                          ComProc
	CapturePreview                         ComProc
	Reload                                 ComProc
	PostWebMessageAsJson                   ComProc
	PostWebMessageAsString                 ComProc
	AddWebMessageReceived                  ComProc
	RemoveWebMessageReceived               ComProc
	CallDevToolsProtocolMethod             ComProc
	GetBrowserProcessId                    ComProc
	GetCanGoBack                           ComProc
	GetCanGoForward                        ComProc
	GoBack                                 ComProc
	GoForward                              ComProc
	GetDevToolsProtocolEventReceiver       ComProc
	Stop                                   ComProc
	AddNewWindowRequested                  ComProc
	RemoveNewWindowRequested               ComProc
	AddDocumentTitleChanged                ComProc
	RemoveDocumentTitleChanged             ComProc
	GetDocumentTitle                       ComProc
	AddHostObjectToScript                  ComProc
	RemoveHostObjectFromScript             ComProc
	OpenDevToolsWindow                     ComProc
	AddContainsFullScreenElementChanged    ComProc
	RemoveContainsFullScreenElementChanged ComProc
	GetContainsFullScreenElement           ComProc
	AddWebResourceRequested                ComProc
	RemoveWebResourceRequested             ComProc
	AddWebResourceRequestedFilter          ComProc
	RemoveWebResourceRequestedFilter       ComProc
	AddWindowCloseRequested                ComProc
	RemoveWindowCloseRequested             ComProc
}

type ICoreWebView2 struct {
	Vtbl *ICoreWebView2Vtbl
}

// GetSettings returns the base settings interface. The pointer is a new
// reference; the caller must Release it. Settings3/5/9 are reached by
// QueryInterface from here.
func (w *ICoreWebView2) GetSettings() (*ICoreWebView2Settings, error) {
	var settings *ICoreWebView2Settings
	hr, _, _ := w.Vtbl.GetSettings.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(&settings)),
	)
	if err := hres(hr); err != nil {
		return nil, err
	}
	if settings == nil {
		return nil, errNilInterface
	}
	return settings, nil
}

func (w *ICoreWebView2) Navigate(uri string) error {
	target, err := wstr(uri)
	if err != nil {
		return err
	}
	hr, _, _ := w.Vtbl.Navigate.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(target)),
	)
	return hres(hr)
}

// AddScriptToExecuteOnDocumentCreated queues a script to run before any page
// script on every future navigation. It only affects navigations that start
// after it is registered, so it has to be called before the first Navigate.
//
// handler receives the script id and may be nil - see the note on ExecuteScript.
func (w *ICoreWebView2) AddScriptToExecuteOnDocumentCreated(script string, handler unsafe.Pointer) error {
	source, err := wstr(script)
	if err != nil {
		return err
	}
	hr, _, _ := w.Vtbl.AddScriptToExecuteOnDocumentCreated.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(source)),
		uintptr(handler),
	)
	return hres(hr)
}

// ExecuteScript runs script in the current document.
//
// handler is the completion callback that receives the script's JSON result.
//
// UNVERIFIED: passing nil for handler. WebView2.idl annotates the parameter
// plainly as `[in] ICoreWebView2ExecuteScriptCompletedHandler* handler`, with
// no [optional] and no [unique], and Microsoft's reference never states that
// NULL is accepted. It is widely done and appears to work, but it is not a
// documented contract, so this binding does not rely on it: pass a handler when
// you need the result, and treat a nil handler as "best effort, unsupported by
// the docs".
func (w *ICoreWebView2) ExecuteScript(script string, handler unsafe.Pointer) error {
	source, err := wstr(script)
	if err != nil {
		return err
	}
	hr, _, _ := w.Vtbl.ExecuteScript.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(source)),
		uintptr(handler),
	)
	return hres(hr)
}

// PostWebMessageAsString delivers message to the page as a string, surfacing on
// window.chrome.webview's message event with .data set to the string.
func (w *ICoreWebView2) PostWebMessageAsString(message string) error {
	payload, err := wstr(message)
	if err != nil {
		return err
	}
	hr, _, _ := w.Vtbl.PostWebMessageAsString.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(payload)),
	)
	return hres(hr)
}

func (w *ICoreWebView2) AddWebMessageReceived(handler unsafe.Pointer) (EventRegistrationToken, error) {
	var token EventRegistrationToken
	hr, _, _ := w.Vtbl.AddWebMessageReceived.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(handler),
		uintptr(unsafe.Pointer(&token)),
	)
	return token, hres(hr)
}

func (w *ICoreWebView2) AddNavigationStarting(handler unsafe.Pointer) (EventRegistrationToken, error) {
	var token EventRegistrationToken
	hr, _, _ := w.Vtbl.AddNavigationStarting.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(handler),
		uintptr(unsafe.Pointer(&token)),
	)
	return token, hres(hr)
}

func (w *ICoreWebView2) AddNavigationCompleted(handler unsafe.Pointer) (EventRegistrationToken, error) {
	var token EventRegistrationToken
	hr, _, _ := w.Vtbl.AddNavigationCompleted.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(handler),
		uintptr(unsafe.Pointer(&token)),
	)
	return token, hres(hr)
}

func (w *ICoreWebView2) AddWebResourceRequested(handler unsafe.Pointer) (EventRegistrationToken, error) {
	var token EventRegistrationToken
	hr, _, _ := w.Vtbl.AddWebResourceRequested.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(handler),
		uintptr(unsafe.Pointer(&token)),
	)
	return token, hres(hr)
}

func (w *ICoreWebView2) AddProcessFailed(handler unsafe.Pointer) (EventRegistrationToken, error) {
	var token EventRegistrationToken
	hr, _, _ := w.Vtbl.AddProcessFailed.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(handler),
		uintptr(unsafe.Pointer(&token)),
	)
	return token, hres(hr)
}

// AddWebResourceRequestedFilter narrows which requests raise
// WebResourceRequested. Without at least one filter the event never fires, so
// this is not optional decoration: it is what turns the handler on.
func (w *ICoreWebView2) AddWebResourceRequestedFilter(uri string, context WebResourceContext) error {
	filter, err := wstr(uri)
	if err != nil {
		return err
	}
	hr, _, _ := w.Vtbl.AddWebResourceRequestedFilter.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(filter)),
		uintptr(context),
	)
	return hres(hr)
}
