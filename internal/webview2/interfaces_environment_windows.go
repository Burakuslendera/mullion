//go:build windows

package webview2

// IStream and ICoreWebView2Environment. Split from interfaces_windows.go,
// whose header carries the ABI contract that governs every vtable struct here.

import (
	"unsafe"
)

// ---------------------------------------------------------------------------
// IStream (objidl.h)
//
// Memory-backed streams come from shlwapi!SHCreateMemStream, so this package
// only ever needs to release them. The full vtable is still declared: Release
// is slot 2 and would be correct even with an empty tail, but leaving the tail
// out would invite someone to add a method later and silently land it in the
// wrong slot.
//
// Layout: IUnknown(3) + ISequentialStream(2) + IStream(9) = 14 slots.
// ---------------------------------------------------------------------------

type ISequentialStreamVtbl struct {
	IUnknownVtbl
	Read  ComProc
	Write ComProc
}

type IStreamVtbl struct {
	ISequentialStreamVtbl
	Seek         ComProc
	SetSize      ComProc
	CopyTo       ComProc
	Commit       ComProc
	Revert       ComProc
	LockRegion   ComProc
	UnlockRegion ComProc
	Stat         ComProc
	Clone        ComProc
}

type IStream struct {
	Vtbl *IStreamVtbl
}

func (s *IStream) Release() error {
	if s == nil {
		return nil
	}
	_, _, _ = s.Vtbl.Release.Call(uintptr(unsafe.Pointer(s)))
	return nil
}

// ---------------------------------------------------------------------------
// ICoreWebView2Environment  {b96d755e-0319-4e92-a296-23436f46a1fc}
// 8 slots: IUnknown(3) + 5.
// ---------------------------------------------------------------------------

type ICoreWebView2EnvironmentVtbl struct {
	IUnknownVtbl
	CreateCoreWebView2Controller     ComProc
	CreateWebResourceResponse        ComProc
	GetBrowserVersionString          ComProc
	AddNewBrowserVersionAvailable    ComProc
	RemoveNewBrowserVersionAvailable ComProc
}

type ICoreWebView2Environment struct {
	Vtbl *ICoreWebView2EnvironmentVtbl
}

// CreateCoreWebView2Controller starts asynchronous controller creation. The
// result arrives on handler, which must be an
// ICoreWebView2CreateCoreWebView2ControllerCompletedHandler COM object; this
// method returning nil only means the request was accepted.
//
// parentWindow is an HWND, kept as uintptr so this file does not have to own a
// window-handle type.
func (e *ICoreWebView2Environment) CreateCoreWebView2Controller(parentWindow uintptr, handler unsafe.Pointer) error {
	hr, _, _ := e.Vtbl.CreateCoreWebView2Controller.Call(
		uintptr(unsafe.Pointer(e)),
		parentWindow,
		uintptr(handler),
	)
	return hres(hr)
}

// CreateWebResourceResponse builds the response handed back to a
// WebResourceRequested event.
//
// content may be nil, which is how a bodyless response (204, or an error page
// with no payload) is expressed. Ownership: the returned response is a new
// reference and the caller must Release it; the runtime AddRefs content itself,
// but the caller still owns its own reference to the stream.
func (e *ICoreWebView2Environment) CreateWebResourceResponse(content *IStream, statusCode int32, reasonPhrase, headers string) (*ICoreWebView2WebResourceResponse, error) {
	reason, err := wstr(reasonPhrase)
	if err != nil {
		return nil, err
	}
	head, err := wstr(headers)
	if err != nil {
		return nil, err
	}
	var response *ICoreWebView2WebResourceResponse
	hr, _, _ := e.Vtbl.CreateWebResourceResponse.Call(
		uintptr(unsafe.Pointer(e)),
		uintptr(unsafe.Pointer(content)),
		uintptr(statusCode),
		uintptr(unsafe.Pointer(reason)),
		uintptr(unsafe.Pointer(head)),
		uintptr(unsafe.Pointer(&response)),
	)
	if err := hres(hr); err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errNilInterface
	}
	return response, nil
}
