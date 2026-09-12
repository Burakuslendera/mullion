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

func (s *IStream) Release() {
	if s == nil {
		return
	}
	_, _, _ = s.Vtbl.Release.Call(uintptr(unsafe.Pointer(s)))
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

// AddRef takes a reference of the caller's own on the environment object.
//
// Browser.Environment hands out an uncounted copy of the interface the Browser
// stores, so a caller that keeps the pointer across a boundary where embedder
// code can run - code that pumps a nested message loop can dispatch WM_DESTROY
// and release the Browser's stored reference mid-call (issue #161) - must take
// one first, per Microsoft's rules for managing reference counts. Pair every
// AddRef with exactly one Release.
func (e *ICoreWebView2Environment) AddRef() {
	if e == nil {
		return
	}
	asUnknown(e).AddRef()
}

// Release drops a reference taken by AddRef.
func (e *ICoreWebView2Environment) Release() {
	if e == nil {
		return
	}
	asUnknown(e).Release()
}

// CreateWebResourceResponse builds the response handed back to a
// WebResourceRequested event.
//
// content may be nil, which is how a bodyless response (204, or an error page
// with no payload) is expressed. Creation takes no reference on content. The
// returned response is a new reference the caller must Release; attach a body
// with PutContent so the response retains the stream.
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
