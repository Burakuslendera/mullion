//go:build windows

package webview2

// Event-argument interfaces: what the runtime hands the message, navigation
// and process-failure callbacks. Split from interfaces_windows.go, whose
// header carries the ABI contract that governs every vtable struct here.

import (
	"unsafe"
)

// ---------------------------------------------------------------------------
// ICoreWebView2WebMessageReceivedEventArgs  {0f99a40c-e962-4207-9e92-e3d542eff849}
// 6 slots.
// ---------------------------------------------------------------------------

type ICoreWebView2WebMessageReceivedEventArgsVtbl struct {
	IUnknownVtbl
	GetSource                ComProc
	GetWebMessageAsJson      ComProc
	TryGetWebMessageAsString ComProc
}

type ICoreWebView2WebMessageReceivedEventArgs struct {
	Vtbl *ICoreWebView2WebMessageReceivedEventArgsVtbl
}

// GetSource is the URI of the document that posted the message. Worth checking
// before trusting a message: it is the only thing distinguishing the app's own
// page from an iframe.
func (a *ICoreWebView2WebMessageReceivedEventArgs) GetSource() (string, error) {
	var source *uint16
	hr, _, _ := a.Vtbl.GetSource.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&source)),
	)
	if err := hres(hr); err != nil {
		return "", err
	}
	return takeWstr(source), nil
}

func (a *ICoreWebView2WebMessageReceivedEventArgs) GetWebMessageAsJson() (string, error) {
	var message *uint16
	hr, _, _ := a.Vtbl.GetWebMessageAsJson.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&message)),
	)
	if err := hres(hr); err != nil {
		return "", err
	}
	return takeWstr(message), nil
}

// TryGetWebMessageAsString fails with E_INVALIDARG when the page posted a
// non-string (postMessage of an object). That is a normal outcome, not a bug -
// callers that accept both shapes should fall back to GetWebMessageAsJson.
func (a *ICoreWebView2WebMessageReceivedEventArgs) TryGetWebMessageAsString() (string, error) {
	var message *uint16
	hr, _, _ := a.Vtbl.TryGetWebMessageAsString.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&message)),
	)
	if err := hres(hr); err != nil {
		return "", err
	}
	return takeWstr(message), nil
}

// ---------------------------------------------------------------------------
// ICoreWebView2NavigationStartingEventArgs  {5b495469-e119-438a-9b18-7604f25f2e49}
// 10 slots. RequestHeaders and the Cancel pair are declared so the slots after
// them stay at the right offsets, but carry no wrappers: this binding reads
// identity (issue #68 follow-up, decisions/0021), and the navigation-cancel
// gate is issue #6's work - PutCancel gets its wrapper there.
// ---------------------------------------------------------------------------

type ICoreWebView2NavigationStartingEventArgsVtbl struct {
	IUnknownVtbl
	GetUri             ComProc
	GetIsUserInitiated ComProc
	GetIsRedirected    ComProc
	GetRequestHeaders  ComProc
	GetCancel          ComProc
	PutCancel          ComProc
	GetNavigationID    ComProc
}

type ICoreWebView2NavigationStartingEventArgs struct {
	Vtbl *ICoreWebView2NavigationStartingEventArgsVtbl
}

// GetUri is the URI of the requested navigation, before it commits.
func (a *ICoreWebView2NavigationStartingEventArgs) GetUri() (string, error) {
	var uri *uint16
	hr, _, _ := a.Vtbl.GetUri.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&uri)),
	)
	if err := hres(hr); err != nil {
		return "", err
	}
	return takeWstr(uri), nil
}

// GetIsUserInitiated reports whether the navigation came from a user gesture.
// The runtime counts navigations issued through WebView2 APIs - the host's own
// Navigate calls - as user initiated too.
func (a *ICoreWebView2NavigationStartingEventArgs) GetIsUserInitiated() (bool, error) {
	var initiated int32
	hr, _, _ := a.Vtbl.GetIsUserInitiated.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&initiated)),
	)
	if err := hres(hr); err != nil {
		return false, err
	}
	return boolFromBOOL(initiated), nil
}

// GetIsRedirected reports whether this start is an HTTP redirect of an earlier
// navigation. A redirect keeps its navigation id, so a correlating caller sees
// the same id start more than once.
func (a *ICoreWebView2NavigationStartingEventArgs) GetIsRedirected() (bool, error) {
	var redirected int32
	hr, _, _ := a.Vtbl.GetIsRedirected.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&redirected)),
	)
	if err := hres(hr); err != nil {
		return false, err
	}
	return boolFromBOOL(redirected), nil
}

// GetNavigationID is the runtime-assigned identity of this navigation. The
// matching completion reports the same id, which is the only channel that ties
// a NavigationCompleted to the Navigate that caused it.
func (a *ICoreWebView2NavigationStartingEventArgs) GetNavigationID() (uint64, error) {
	var id uint64
	hr, _, _ := a.Vtbl.GetNavigationID.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&id)),
	)
	if err := hres(hr); err != nil {
		return 0, err
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// ICoreWebView2NavigationCompletedEventArgs  {30d68b7d-20d9-4752-a9ca-ec8448fbb5c1}
// 6 slots.
// ---------------------------------------------------------------------------

type ICoreWebView2NavigationCompletedEventArgsVtbl struct {
	IUnknownVtbl
	GetIsSuccess      ComProc
	GetWebErrorStatus ComProc
	GetNavigationID   ComProc
}

type ICoreWebView2NavigationCompletedEventArgs struct {
	Vtbl *ICoreWebView2NavigationCompletedEventArgsVtbl
}

func (a *ICoreWebView2NavigationCompletedEventArgs) GetIsSuccess() (bool, error) {
	var success int32
	hr, _, _ := a.Vtbl.GetIsSuccess.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&success)),
	)
	if err := hres(hr); err != nil {
		return false, err
	}
	return boolFromBOOL(success), nil
}

func (a *ICoreWebView2NavigationCompletedEventArgs) GetWebErrorStatus() (WebErrorStatus, error) {
	var status WebErrorStatus
	hr, _, _ := a.Vtbl.GetWebErrorStatus.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&status)),
	)
	if err := hres(hr); err != nil {
		return 0, err
	}
	return status, nil
}

// GetNavigationID is the identity the matching NavigationStarting reported;
// see ICoreWebView2NavigationStartingEventArgs.GetNavigationID.
func (a *ICoreWebView2NavigationCompletedEventArgs) GetNavigationID() (uint64, error) {
	var id uint64
	hr, _, _ := a.Vtbl.GetNavigationID.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&id)),
	)
	if err := hres(hr); err != nil {
		return 0, err
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// ICoreWebView2ProcessFailedEventArgs  {8155a9a4-1474-4a86-8cae-151b0fa6b8ca}
// 4 slots. Later revisions (ProcessFailedEventArgs2/3) add more; this binding
// stays on the base interface because the kind is all the host reports.
// ---------------------------------------------------------------------------

type ICoreWebView2ProcessFailedEventArgsVtbl struct {
	IUnknownVtbl
	GetProcessFailedKind ComProc
}

type ICoreWebView2ProcessFailedEventArgs struct {
	Vtbl *ICoreWebView2ProcessFailedEventArgsVtbl
}

func (a *ICoreWebView2ProcessFailedEventArgs) GetProcessFailedKind() (ProcessFailedKind, error) {
	var kind ProcessFailedKind
	hr, _, _ := a.Vtbl.GetProcessFailedKind.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&kind)),
	)
	if err := hres(hr); err != nil {
		return 0, err
	}
	return kind, nil
}
