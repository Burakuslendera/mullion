//go:build windows

package webview2

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Feature detection in this package is QueryInterface, never a version compare.
//
// That is not a style preference. This package talks to the runtime's client DLL
// directly instead of going through the SDK loader, and the loader is where the
// minimum-version gate lives - bypass it and the gate is bypassed too. So a
// version number proves nothing here; asking the object whether it implements an
// interface proves everything. An old runtime answers E_NOINTERFACE, which is a
// clean "no", not a crash.

var procSHCreateMemStream = windows.NewLazySystemDLL("shlwapi.dll").NewProc("SHCreateMemStream")

// asUnknown reinterprets a COM interface pointer as IUnknown.
//
// Every interface in this package is a struct whose first and only field is its
// vtable pointer, and every COM vtable begins with the three IUnknown slots, so
// the cast is exactly what the ABI already guarantees.
func asUnknown[T any](iface *T) *IUnknown {
	return (*IUnknown)(unsafe.Pointer(iface))
}

func queryAs[T any, S any](iface *S, iid *windows.GUID) (*T, error) {
	if iface == nil {
		return nil, errors.New("webview2: query interface on a nil object")
	}
	ptr, err := asUnknown(iface).QueryInterface(iid)
	if err != nil {
		return nil, err
	}
	return (*T)(ptr), nil
}

// Interface exposes the environment as its typed COM interface.
func (e *Environment) Interface() *ICoreWebView2Environment {
	if e == nil {
		return nil
	}
	return (*ICoreWebView2Environment)(unsafe.Pointer(e.Unknown()))
}

// QueryController2 asks for ICoreWebView2Controller2, where the default
// background colour lives. The caller must Release the result: QueryInterface
// AddRefs.
func (c *ICoreWebView2Controller) QueryController2() (*ICoreWebView2Controller2, error) {
	return queryAs[ICoreWebView2Controller2](c, &IIDICoreWebView2Controller2)
}

// QueryController3 asks for ICoreWebView2Controller3, where the bounds mode and
// the monitor-scale policy live.
func (c *ICoreWebView2Controller) QueryController3() (*ICoreWebView2Controller3, error) {
	return queryAs[ICoreWebView2Controller3](c, &IIDICoreWebView2Controller3)
}

// QuerySettings3 asks for ICoreWebView2Settings3, where the browser accelerator
// keys live.
func (s *ICoreWebView2Settings) QuerySettings3() (*ICoreWebView2Settings3, error) {
	return queryAs[ICoreWebView2Settings3](s, &IIDICoreWebView2Settings3)
}

// QuerySettings5 asks for ICoreWebView2Settings5, where pinch zoom lives.
func (s *ICoreWebView2Settings) QuerySettings5() (*ICoreWebView2Settings5, error) {
	return queryAs[ICoreWebView2Settings5](s, &IIDICoreWebView2Settings5)
}

// QuerySettings9 asks for ICoreWebView2Settings9, where non-client region
// support lives. A runtime older than 131.0.2903.40 does not implement it, and
// this is the supported way to tell the difference.
func (s *ICoreWebView2Settings) QuerySettings9() (*ICoreWebView2Settings9, error) {
	return queryAs[ICoreWebView2Settings9](s, &IIDICoreWebView2Settings9)
}

// Release drops the reference GetSettings returned. Unlike its Query* siblings
// below, the base settings object comes from ICoreWebView2.GetSettings rather
// than QueryInterface, but the ownership is the same: the getter AddRefs on the
// way out, so every call pairs with exactly one Release.
func (s *ICoreWebView2Settings) Release() { asUnknown(s).Release() }

// Release drops a reference obtained from QueryInterface.
func (c *ICoreWebView2Controller2) Release() { asUnknown(c).Release() }

// Release drops a reference obtained from QueryInterface.
func (c *ICoreWebView2Controller3) Release() { asUnknown(c).Release() }

// Release drops a reference obtained from QueryInterface.
func (s *ICoreWebView2Settings3) Release() { asUnknown(s).Release() }

// Release drops a reference obtained from QueryInterface.
func (s *ICoreWebView2Settings5) Release() { asUnknown(s).Release() }

// Release drops a reference obtained from QueryInterface.
func (s *ICoreWebView2Settings9) Release() { asUnknown(s).Release() }

// NewMemoryStream copies content into a COM memory stream.
//
// The bytes have to be copied because the response outlives the call that
// produced it: the runtime reads the body asynchronously, long after the
// WebResourceRequested handler has returned. A stream over a Go slice would be
// a promise the garbage collector does not keep.
func NewMemoryStream(content []byte) (*IStream, error) {
	var data uintptr
	if len(content) > 0 {
		data = uintptr(unsafe.Pointer(&content[0]))
	}
	result, _, err := procSHCreateMemStream.Call(data, uintptr(len(content)))
	// The syscall only ever saw a uintptr, which the collector does not treat as
	// a reference; keep the slice alive until the copy is done.
	runtime.KeepAlive(content)
	if result == 0 {
		if err == nil || errors.Is(err, windows.ERROR_SUCCESS) {
			return nil, errors.New("webview2: SHCreateMemStream returned no stream")
		}
		return nil, err
	}
	// The address is turned into a typed pointer through the same copy the rest
	// of the package uses, rather than a direct cast: it points into COM memory,
	// never into the Go heap, and casting a uintptr is exactly the pattern the
	// compiler cannot verify.
	return (*IStream)(unsafe.Pointer(unknownFromAddress(result))), nil
}
