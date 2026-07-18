//go:build windows

package webview2

// The web-resource interception surface: the request, its event args, and the
// response. Split from interfaces_windows.go, whose header carries the ABI
// contract that governs every vtable struct here.

import (
	"unsafe"
)

// ---------------------------------------------------------------------------
// ICoreWebView2WebResourceRequest  {97055cd4-512c-4264-8b5f-e3f446cea6a5}
// 10 slots.
// ---------------------------------------------------------------------------

type ICoreWebView2WebResourceRequestVtbl struct {
	IUnknownVtbl
	GetUri     ComProc
	PutUri     ComProc
	GetMethod  ComProc
	PutMethod  ComProc
	GetContent ComProc
	PutContent ComProc
	GetHeaders ComProc
}

type ICoreWebView2WebResourceRequest struct {
	Vtbl *ICoreWebView2WebResourceRequestVtbl
}

func (r *ICoreWebView2WebResourceRequest) GetUri() (string, error) {
	var uri *uint16
	hr, _, _ := r.Vtbl.GetUri.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(&uri)),
	)
	if err := hres(hr); err != nil {
		return "", err
	}
	return takeWstr(uri), nil
}

func (r *ICoreWebView2WebResourceRequest) GetMethod() (string, error) {
	var method *uint16
	hr, _, _ := r.Vtbl.GetMethod.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(&method)),
	)
	if err := hres(hr); err != nil {
		return "", err
	}
	return takeWstr(method), nil
}

// ---------------------------------------------------------------------------
// ICoreWebView2WebResourceRequestedEventArgs  {453e667f-12c7-49d4-be6d-ddbe7956f57a}
// 8 slots.
// ---------------------------------------------------------------------------

type ICoreWebView2WebResourceRequestedEventArgsVtbl struct {
	IUnknownVtbl
	GetRequest         ComProc
	GetResponse        ComProc
	PutResponse        ComProc
	GetDeferral        ComProc
	GetResourceContext ComProc
}

type ICoreWebView2WebResourceRequestedEventArgs struct {
	Vtbl *ICoreWebView2WebResourceRequestedEventArgsVtbl
}

// GetRequest returns a new reference; the caller must Release it.
func (a *ICoreWebView2WebResourceRequestedEventArgs) GetRequest() (*ICoreWebView2WebResourceRequest, error) {
	var request *ICoreWebView2WebResourceRequest
	hr, _, _ := a.Vtbl.GetRequest.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&request)),
	)
	if err := hres(hr); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, errNilInterface
	}
	return request, nil
}

// PutResponse hands the response back to the runtime. The runtime AddRefs it,
// but the caller still owns the reference it got from CreateWebResourceResponse
// and must Release that one.
func (a *ICoreWebView2WebResourceRequestedEventArgs) PutResponse(response *ICoreWebView2WebResourceResponse) error {
	hr, _, _ := a.Vtbl.PutResponse.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(response)),
	)
	return hres(hr)
}

// ---------------------------------------------------------------------------
// ICoreWebView2WebResourceResponse  {aafcc94f-fa27-48fd-97df-830ef75aaec9}
// 10 slots.
//
// Slot order trap: Headers sits BETWEEN Content and StatusCode. Grouping the
// obvious way (Content, StatusCode, ReasonPhrase, Headers) shifts three slots.
// ---------------------------------------------------------------------------

type ICoreWebView2WebResourceResponseVtbl struct {
	IUnknownVtbl
	GetContent      ComProc
	PutContent      ComProc
	GetHeaders      ComProc
	GetStatusCode   ComProc
	PutStatusCode   ComProc
	GetReasonPhrase ComProc
	PutReasonPhrase ComProc
}

type ICoreWebView2WebResourceResponse struct {
	Vtbl *ICoreWebView2WebResourceResponseVtbl
}

// PutContent attaches the body. The runtime takes its own reference on the
// stream, but the caller must keep its reference alive until the response has
// been consumed, then Release both.
func (r *ICoreWebView2WebResourceResponse) PutContent(content *IStream) error {
	hr, _, _ := r.Vtbl.PutContent.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(content)),
	)
	return hres(hr)
}

func (r *ICoreWebView2WebResourceResponse) GetStatusCode() (int32, error) {
	var status int32
	hr, _, _ := r.Vtbl.GetStatusCode.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(&status)),
	)
	if err := hres(hr); err != nil {
		return 0, err
	}
	return status, nil
}

func (r *ICoreWebView2WebResourceResponse) PutStatusCode(status int32) error {
	hr, _, _ := r.Vtbl.PutStatusCode.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(status),
	)
	return hres(hr)
}

func (r *ICoreWebView2WebResourceResponse) GetReasonPhrase() (string, error) {
	var reason *uint16
	hr, _, _ := r.Vtbl.GetReasonPhrase.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(&reason)),
	)
	if err := hres(hr); err != nil {
		return "", err
	}
	return takeWstr(reason), nil
}

func (r *ICoreWebView2WebResourceResponse) PutReasonPhrase(reason string) error {
	phrase, err := wstr(reason)
	if err != nil {
		return err
	}
	hr, _, _ := r.Vtbl.PutReasonPhrase.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(phrase)),
	)
	return hres(hr)
}

// GetHeaders is intentionally left unwrapped: it yields an
// ICoreWebView2HttpResponseHeaders, and this package sets headers as a raw
// string through CreateWebResourceResponse instead, so binding that interface
// would add ABI surface with no caller. The SLOT still has to exist - removing
// it would shift StatusCode and everything after it.

func (r *ICoreWebView2WebResourceResponse) Release() error {
	if r == nil {
		return nil
	}
	_, _, _ = r.Vtbl.Release.Call(uintptr(unsafe.Pointer(r)))
	return nil
}
