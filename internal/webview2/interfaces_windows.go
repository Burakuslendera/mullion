//go:build windows

// Package webview2 contains hand-written COM bindings for the WebView2 Win32 API.
//
// This file declares the interface vtables and the Go method wrappers. Every
// layout here is derived from Microsoft's official WebView2 SDK
// (Microsoft.Web.WebView2, build/native/include/WebView2.h and WebView2.idl),
// which is the MIDL-generated C ABI. Nothing is copied from a third-party Go
// binding.
//
// # ABI contract (read before touching any struct in this file)
//
// COM dispatches by vtable *offset*, not by name. Adding, removing, reordering
// or misspelling-into-the-wrong-slot a single ComProc field silently retargets
// every method after it. The failure mode is not an error return: it is a call
// through a wrong function pointer, i.e. memory corruption or a hard crash
// inside the runtime, at the point of first use.
//
// Two rules follow, and both are load-bearing:
//
//  1. Vtables list EVERY method of the interface, in IDL order, including the
//     ones this package never calls. Unused slots are still declared, because
//     the slots after them depend on their presence. Do not "clean up" a field
//     just because nothing references it.
//  2. Each interface embeds its base interface's vtable, mirroring the C++
//     inheritance chain (ICoreWebView2Settings9 : ...8 : ...7 : ... : IUnknown).
//     Go lays embedded structs out inline and in order, so the embedding chain
//     reproduces exactly the flattened vtable MIDL emits.
//
// interfaces_windows_test.go pins every slot offset and every IID with
// unsafe.Offsetof, and runs without a WebView2 runtime. Re-run it after any
// edit here; a passing build proves nothing about a vtable.
//
// # Win64 argument-passing rules that matter here
//
// From the x64 calling convention (learn.microsoft.com/cpp/build/x64-calling-convention):
// "Structs and unions of size 8, 16, 32, or 64 bits ... are passed as if they
// were integers of the same size. Structs or unions of other sizes are passed
// as a pointer to memory allocated by the caller."
//
//   - COREWEBVIEW2_COLOR is 4 bytes (32 bits) -> passed BY VALUE, packed into a
//     register. See Color.pack.
//   - RECT is 16 bytes (128 bits) -> passed BY POINTER, even though the C
//     signature says `put_Bounds(RECT bounds)`. See ICoreWebView2Controller.PutBounds.
//   - `double` lands in XMM1 for the second argument. Go's syscall bridge copies
//     the first four integer-register arguments into X0-X3 precisely so that
//     floating-point arguments work (see Go's
//     internal/runtime/syscall/windows/asm_windows_amd64.s: "Floating point
//     arguments are passed in the XMM registers. Set them here in case any of
//     the arguments are floating point values."). So passing
//     math.Float64bits(v) as a uintptr reaches the callee correctly. See
//     ICoreWebView2Controller3.PutRasterizationScale.
package webview2

import (
	"errors"
	"math"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------------------------------------------------------------------------
// Value types
// ---------------------------------------------------------------------------

// Rect is the Win32 RECT. Layout is ABI-critical: four 32-bit fields, 16 bytes.
type Rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// Color is COREWEBVIEW2_COLOR.
//
// Field order is A,R,G,B - NOT R,G,B,A. This is a real trap: the struct is
// declared `{ BYTE A; BYTE R; BYTE G; BYTE B; }` in WebView2.idl, so a binding
// that reorders the fields compiles fine and silently renders the wrong colour
// (and, worse, the wrong alpha - swapping A and B turns an opaque background
// transparent).
type Color struct {
	A byte
	R byte
	G byte
	B byte
}

// pack flattens Color into the single register the Win64 ABI passes it in.
//
// The struct is 32 bits, so it travels by value "as if it were an integer of
// the same size". Little-endian: A is the lowest-addressed byte and therefore
// the least significant. TestColorPackMatchesMemoryLayout pins this against an
// unsafe reinterpretation of the struct, so the two can never drift.
func (c Color) pack() uintptr {
	return uintptr(uint32(c.A) | uint32(c.R)<<8 | uint32(c.G)<<16 | uint32(c.B)<<24)
}

// EventRegistrationToken is the token an add_* method writes back. The C type is
// a struct wrapping a single __int64; an 8-byte scalar is layout-identical and
// is always passed by pointer, so nothing is lost by flattening it.
type EventRegistrationToken int64

// BoundsMode is COREWEBVIEW2_BOUNDS_MODE.
type BoundsMode int32

const (
	// BoundsModeUseRawPixels makes Bounds mean physical (device) pixels.
	//
	// This is the mode this host wants: it computes bounds from the window's
	// client rect, which is already in physical pixels, and manages DPI itself.
	// The alternative would have WebView2 rescale bounds by RasterizationScale
	// behind our back, which double-applies DPI.
	BoundsModeUseRawPixels BoundsMode = 0
	// BoundsModeUseRasterizationScale makes Bounds be interpreted in logical
	// pixels, scaled by RasterizationScale.
	BoundsModeUseRasterizationScale BoundsMode = 1
)

// WebResourceContext is COREWEBVIEW2_WEB_RESOURCE_CONTEXT.
type WebResourceContext int32

// Only the values this package uses are named. The full enum runs to
// COREWEBVIEW2_WEB_RESOURCE_CONTEXT_OTHER = 16.
const (
	// WebResourceContextAll matches every request kind. Verified = 0 in
	// WebView2.h; it is the first enumerator, not a bitmask of the others, so
	// it must not be OR-ed with anything.
	WebResourceContextAll      WebResourceContext = 0
	WebResourceContextDocument WebResourceContext = 1
)

// ProcessFailedKind is COREWEBVIEW2_PROCESS_FAILED_KIND.
type ProcessFailedKind int32

const (
	ProcessFailedKindBrowserProcessExited      ProcessFailedKind = 0
	ProcessFailedKindRenderProcessExited       ProcessFailedKind = 1
	ProcessFailedKindRenderProcessUnresponsive ProcessFailedKind = 2
)

// WebErrorStatus is COREWEBVIEW2_WEB_ERROR_STATUS. Only carried through, never
// interpreted here, so the enumerators are not mirrored.
type WebErrorStatus int32

// ---------------------------------------------------------------------------
// Interface IDs
//
// Transcribed from the MIDL_INTERFACE attributes in WebView2.h. A single
// swapped nibble compiles fine and only shows up as a QueryInterface miss at
// runtime, so interfaces_windows_test.go re-parses each one from its canonical
// string form and compares.
// ---------------------------------------------------------------------------

var (
	// IIDICoreWebView2Settings3 = {fdb5ab74-af33-4854-84f0-0a631deb5eba}
	IIDICoreWebView2Settings3 = windows.GUID{
		Data1: 0xfdb5ab74, Data2: 0xaf33, Data3: 0x4854,
		Data4: [8]byte{0x84, 0xf0, 0x0a, 0x63, 0x1d, 0xeb, 0x5e, 0xba},
	}
	// IIDICoreWebView2Settings5 = {183e7052-1d03-43a0-ab99-98e043b66b39}
	IIDICoreWebView2Settings5 = windows.GUID{
		Data1: 0x183e7052, Data2: 0x1d03, Data3: 0x43a0,
		Data4: [8]byte{0xab, 0x99, 0x98, 0xe0, 0x43, 0xb6, 0x6b, 0x39},
	}
	// IIDICoreWebView2Settings9 = {0528a73b-e92d-49f4-927a-e547dddaa37d}
	// Requires WebView2 Runtime 1.0.2420.47+ (the app-region / non-client
	// region support release).
	IIDICoreWebView2Settings9 = windows.GUID{
		Data1: 0x0528a73b, Data2: 0xe92d, Data3: 0x49f4,
		Data4: [8]byte{0x92, 0x7a, 0xe5, 0x47, 0xdd, 0xda, 0xa3, 0x7d},
	}
	// IIDICoreWebView2Controller2 = {c979903e-d4ca-4228-92eb-47ee3fa96eab}
	IIDICoreWebView2Controller2 = windows.GUID{
		Data1: 0xc979903e, Data2: 0xd4ca, Data3: 0x4228,
		Data4: [8]byte{0x92, 0xeb, 0x47, 0xee, 0x3f, 0xa9, 0x6e, 0xab},
	}
	// IIDICoreWebView2Controller3 = {f9614724-5d2b-41dc-aef7-73d62b51543b}
	IIDICoreWebView2Controller3 = windows.GUID{
		Data1: 0xf9614724, Data2: 0x5d2b, Data3: 0x41dc,
		Data4: [8]byte{0xae, 0xf7, 0x73, 0xd6, 0x2b, 0x51, 0x54, 0x3b},
	}
)

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

// errNilInterface guards the case where the runtime hands back S_OK with a null
// out-pointer. Dereferencing that would call through a nil vtable.
var errNilInterface = errors.New("webview2: COM call succeeded but returned a nil interface")

// wstr converts to a NUL-terminated UTF-16 string for an LPCWSTR parameter.
func wstr(s string) (*uint16, error) {
	return windows.UTF16PtrFromString(s)
}

// takeWstr converts an LPWSTR *output* to a Go string and frees it.
//
// Every WebView2 get_ method that returns LPWSTR transfers ownership of a
// CoTaskMemAlloc'd buffer to the caller. Not calling CoTaskMemFree leaks it on
// every single call - and these sit on hot paths (get_Uri fires for every web
// resource request), so the leak is unbounded, not a one-off.
func takeWstr(p *uint16) string {
	if p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

// boolToBOOL widens a Go bool to a Win32 BOOL argument.
func boolToBOOL(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// boolFromBOOL narrows a Win32 BOOL out-parameter. BOOL is a 32-bit int, and
// TRUE is "non-zero", not "== 1".
func boolFromBOOL(v int32) bool { return v != 0 }

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

// ---------------------------------------------------------------------------
// ICoreWebView2Controller   {4d00c0d1-9434-4eb6-8078-8697a560334f}  26 slots
// ICoreWebView2Controller2  {c979903e-d4ca-4228-92eb-47ee3fa96eab}  28 slots
// ICoreWebView2Controller3  {f9614724-5d2b-41dc-aef7-73d62b51543b}  36 slots
//
// Note the IDL order: IsVisible comes BEFORE Bounds. Writing the pair the other
// way round (the order they are usually talked about) shifts every later slot.
// ---------------------------------------------------------------------------

type ICoreWebView2ControllerVtbl struct {
	IUnknownVtbl
	GetIsVisible                      ComProc
	PutIsVisible                      ComProc
	GetBounds                         ComProc
	PutBounds                         ComProc
	GetZoomFactor                     ComProc
	PutZoomFactor                     ComProc
	AddZoomFactorChanged              ComProc
	RemoveZoomFactorChanged           ComProc
	SetBoundsAndZoomFactor            ComProc
	MoveFocus                         ComProc
	AddMoveFocusRequested             ComProc
	RemoveMoveFocusRequested          ComProc
	AddGotFocus                       ComProc
	RemoveGotFocus                    ComProc
	AddLostFocus                      ComProc
	RemoveLostFocus                   ComProc
	AddAcceleratorKeyPressed          ComProc
	RemoveAcceleratorKeyPressed       ComProc
	GetParentWindow                   ComProc
	PutParentWindow                   ComProc
	NotifyParentWindowPositionChanged ComProc
	Close                             ComProc
	GetCoreWebView2                   ComProc
}

type ICoreWebView2Controller struct {
	Vtbl *ICoreWebView2ControllerVtbl
}

func (c *ICoreWebView2Controller) GetBounds() (Rect, error) {
	var bounds Rect
	hr, _, _ := c.Vtbl.GetBounds.Call(
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&bounds)),
	)
	if err := hres(hr); err != nil {
		return Rect{}, err
	}
	return bounds, nil
}

// PutBounds sets the WebView rect.
//
// ABI: the C signature is `put_Bounds(RECT bounds)` - by value - but RECT is 16
// bytes, and Win64 passes any aggregate that is not 1/2/4/8 bytes as a pointer
// to caller-allocated memory. So the argument really is &bounds. Passing the
// struct's contents inline instead would put Left/Top in the register the
// callee reads as a RECT*, i.e. dereference 0x00000000_00000000 or worse.
func (c *ICoreWebView2Controller) PutBounds(bounds Rect) error {
	hr, _, _ := c.Vtbl.PutBounds.Call(
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&bounds)),
	)
	return hres(hr)
}

func (c *ICoreWebView2Controller) GetIsVisible() (bool, error) {
	var visible int32
	hr, _, _ := c.Vtbl.GetIsVisible.Call(
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&visible)),
	)
	if err := hres(hr); err != nil {
		return false, err
	}
	return boolFromBOOL(visible), nil
}

func (c *ICoreWebView2Controller) PutIsVisible(visible bool) error {
	hr, _, _ := c.Vtbl.PutIsVisible.Call(
		uintptr(unsafe.Pointer(c)),
		boolToBOOL(visible),
	)
	return hres(hr)
}

// GetCoreWebView2 returns a new reference; the caller must Release it.
func (c *ICoreWebView2Controller) GetCoreWebView2() (*ICoreWebView2, error) {
	var view *ICoreWebView2
	hr, _, _ := c.Vtbl.GetCoreWebView2.Call(
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&view)),
	)
	if err := hres(hr); err != nil {
		return nil, err
	}
	if view == nil {
		return nil, errNilInterface
	}
	return view, nil
}

// NotifyParentWindowPositionChanged keeps WebView2's idea of where it is on
// screen in sync. Without it the control renders in the right place but places
// popups, IME candidate windows and the on-screen keyboard against a stale
// origin.
func (c *ICoreWebView2Controller) NotifyParentWindowPositionChanged() error {
	hr, _, _ := c.Vtbl.NotifyParentWindowPositionChanged.Call(uintptr(unsafe.Pointer(c)))
	return hres(hr)
}

func (c *ICoreWebView2Controller) Close() error {
	hr, _, _ := c.Vtbl.Close.Call(uintptr(unsafe.Pointer(c)))
	return hres(hr)
}

// --- Controller2 ---

type ICoreWebView2Controller2Vtbl struct {
	ICoreWebView2ControllerVtbl
	GetDefaultBackgroundColor ComProc
	PutDefaultBackgroundColor ComProc
}

type ICoreWebView2Controller2 struct {
	Vtbl *ICoreWebView2Controller2Vtbl
}

// PutDefaultBackgroundColor sets the colour painted behind the document, i.e.
// what is on screen between controller creation and first paint.
//
// ABI: COREWEBVIEW2_COLOR is 4 bytes, so unlike RECT it goes by VALUE, packed
// into one register. See Color.pack.
func (c *ICoreWebView2Controller2) PutDefaultBackgroundColor(color Color) error {
	hr, _, _ := c.Vtbl.PutDefaultBackgroundColor.Call(
		uintptr(unsafe.Pointer(c)),
		color.pack(),
	)
	return hres(hr)
}

// --- Controller3 ---

type ICoreWebView2Controller3Vtbl struct {
	ICoreWebView2Controller2Vtbl
	GetRasterizationScale              ComProc
	PutRasterizationScale              ComProc
	GetShouldDetectMonitorScaleChanges ComProc
	PutShouldDetectMonitorScaleChanges ComProc
	AddRasterizationScaleChanged       ComProc
	RemoveRasterizationScaleChanged    ComProc
	GetBoundsMode                      ComProc
	PutBoundsMode                      ComProc
}

type ICoreWebView2Controller3 struct {
	Vtbl *ICoreWebView2Controller3Vtbl
}

func (c *ICoreWebView2Controller3) GetRasterizationScale() (float64, error) {
	var scale float64
	hr, _, _ := c.Vtbl.GetRasterizationScale.Call(
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&scale)),
	)
	if err := hres(hr); err != nil {
		return 0, err
	}
	return scale, nil
}

// PutRasterizationScale sets the scale WebView2 rasterizes at.
//
// ABI: `double` is the second argument, so the callee reads it from XMM1, not
// RDX. That works here only because Go's syscall bridge mirrors the first four
// integer-register arguments into X0-X3 for exactly this case (see the package
// comment). math.Float64bits reinterprets the bits without converting them -
// uintptr(scale) would truncate 1.5 to 1.
func (c *ICoreWebView2Controller3) PutRasterizationScale(scale float64) error {
	hr, _, _ := c.Vtbl.PutRasterizationScale.Call(
		uintptr(unsafe.Pointer(c)),
		uintptr(math.Float64bits(scale)),
	)
	return hres(hr)
}

// PutShouldDetectMonitorScaleChanges must be FALSE for this host: it owns DPI
// handling and feeds WebView2 raw pixels. Left TRUE, WebView2 would also react
// to monitor scale changes and re-scale on top of what the host already did.
func (c *ICoreWebView2Controller3) PutShouldDetectMonitorScaleChanges(detect bool) error {
	hr, _, _ := c.Vtbl.PutShouldDetectMonitorScaleChanges.Call(
		uintptr(unsafe.Pointer(c)),
		boolToBOOL(detect),
	)
	return hres(hr)
}

func (c *ICoreWebView2Controller3) GetBoundsMode() (BoundsMode, error) {
	var mode BoundsMode
	hr, _, _ := c.Vtbl.GetBoundsMode.Call(
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&mode)),
	)
	if err := hres(hr); err != nil {
		return 0, err
	}
	return mode, nil
}

// PutBoundsMode pairs with PutShouldDetectMonitorScaleChanges(false):
// BoundsModeUseRawPixels tells WebView2 that the rect it is given is already in
// physical pixels.
func (c *ICoreWebView2Controller3) PutBoundsMode(mode BoundsMode) error {
	hr, _, _ := c.Vtbl.PutBoundsMode.Call(
		uintptr(unsafe.Pointer(c)),
		uintptr(mode),
	)
	return hres(hr)
}

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

// ---------------------------------------------------------------------------
// ICoreWebView2Settings chain  {e562e4f0-d7fa-43ac-8d71-c05150499f00}
//
// Slot budget, which the test pins:
//   IUnknown(3) + base(18) + S2(2) + S3(2) + S4(4) + S5(2) + S6(2) + S7(2)
//              + S8(2) + S9(2) = 39
//
// Property-to-interface map, verified against MS Learn (getting this wrong is
// the classic way to shift the tail):
//   S2 UserAgent | S3 AreBrowserAcceleratorKeysEnabled
//   S4 IsPasswordAutosaveEnabled + IsGeneralAutofillEnabled  (FOUR slots)
//   S5 IsPinchZoomEnabled        | S6 IsSwipeNavigationEnabled
//   S7 HiddenPdfToolbarItems     | S8 IsReputationCheckingRequired
//   S9 IsNonClientRegionSupportEnabled
//
// S5 is pinch zoom and S6 is swipe navigation, not the other way round.
// ---------------------------------------------------------------------------

type ICoreWebView2SettingsVtbl struct {
	IUnknownVtbl
	GetIsScriptEnabled                ComProc
	PutIsScriptEnabled                ComProc
	GetIsWebMessageEnabled            ComProc
	PutIsWebMessageEnabled            ComProc
	GetAreDefaultScriptDialogsEnabled ComProc
	PutAreDefaultScriptDialogsEnabled ComProc
	GetIsStatusBarEnabled             ComProc
	PutIsStatusBarEnabled             ComProc
	GetAreDevToolsEnabled             ComProc
	PutAreDevToolsEnabled             ComProc
	GetAreDefaultContextMenusEnabled  ComProc
	PutAreDefaultContextMenusEnabled  ComProc
	GetAreHostObjectsAllowed          ComProc
	PutAreHostObjectsAllowed          ComProc
	GetIsZoomControlEnabled           ComProc
	PutIsZoomControlEnabled           ComProc
	GetIsBuiltInErrorPageEnabled      ComProc
	PutIsBuiltInErrorPageEnabled      ComProc
}

type ICoreWebView2Settings2Vtbl struct {
	ICoreWebView2SettingsVtbl
	GetUserAgent ComProc
	PutUserAgent ComProc
}

type ICoreWebView2Settings3Vtbl struct {
	ICoreWebView2Settings2Vtbl
	GetAreBrowserAcceleratorKeysEnabled ComProc
	PutAreBrowserAcceleratorKeysEnabled ComProc
}

type ICoreWebView2Settings4Vtbl struct {
	ICoreWebView2Settings3Vtbl
	GetIsPasswordAutosaveEnabled ComProc
	PutIsPasswordAutosaveEnabled ComProc
	GetIsGeneralAutofillEnabled  ComProc
	PutIsGeneralAutofillEnabled  ComProc
}

type ICoreWebView2Settings5Vtbl struct {
	ICoreWebView2Settings4Vtbl
	GetIsPinchZoomEnabled ComProc
	PutIsPinchZoomEnabled ComProc
}

type ICoreWebView2Settings6Vtbl struct {
	ICoreWebView2Settings5Vtbl
	GetIsSwipeNavigationEnabled ComProc
	PutIsSwipeNavigationEnabled ComProc
}

type ICoreWebView2Settings7Vtbl struct {
	ICoreWebView2Settings6Vtbl
	GetHiddenPdfToolbarItems ComProc
	PutHiddenPdfToolbarItems ComProc
}

type ICoreWebView2Settings8Vtbl struct {
	ICoreWebView2Settings7Vtbl
	GetIsReputationCheckingRequired ComProc
	PutIsReputationCheckingRequired ComProc
}

type ICoreWebView2Settings9Vtbl struct {
	ICoreWebView2Settings8Vtbl
	GetIsNonClientRegionSupportEnabled ComProc
	PutIsNonClientRegionSupportEnabled ComProc
}

type ICoreWebView2Settings struct {
	Vtbl *ICoreWebView2SettingsVtbl
}

type ICoreWebView2Settings3 struct {
	Vtbl *ICoreWebView2Settings3Vtbl
}

type ICoreWebView2Settings5 struct {
	Vtbl *ICoreWebView2Settings5Vtbl
}

type ICoreWebView2Settings9 struct {
	Vtbl *ICoreWebView2Settings9Vtbl
}

// Base settings. Every one of these is applied on the NEXT navigation, so they
// must be set between controller creation and the first Navigate.

func (s *ICoreWebView2Settings) PutAreDevToolsEnabled(enabled bool) error {
	hr, _, _ := s.Vtbl.PutAreDevToolsEnabled.Call(uintptr(unsafe.Pointer(s)), boolToBOOL(enabled))
	return hres(hr)
}

func (s *ICoreWebView2Settings) PutAreDefaultContextMenusEnabled(enabled bool) error {
	hr, _, _ := s.Vtbl.PutAreDefaultContextMenusEnabled.Call(uintptr(unsafe.Pointer(s)), boolToBOOL(enabled))
	return hres(hr)
}

func (s *ICoreWebView2Settings) PutIsStatusBarEnabled(enabled bool) error {
	hr, _, _ := s.Vtbl.PutIsStatusBarEnabled.Call(uintptr(unsafe.Pointer(s)), boolToBOOL(enabled))
	return hres(hr)
}

func (s *ICoreWebView2Settings) PutIsZoomControlEnabled(enabled bool) error {
	hr, _, _ := s.Vtbl.PutIsZoomControlEnabled.Call(uintptr(unsafe.Pointer(s)), boolToBOOL(enabled))
	return hres(hr)
}

// PutIsScriptEnabled and PutIsWebMessageEnabled are deliberately NOT wrapped.
// Both default to TRUE and both are load-bearing for this host - turning script
// off kills the frontend, turning web messages off kills the bridge. Leaving
// them unwrapped means no caller can switch them off by accident.

// Settings3.

func (s *ICoreWebView2Settings3) PutAreBrowserAcceleratorKeysEnabled(enabled bool) error {
	hr, _, _ := s.Vtbl.PutAreBrowserAcceleratorKeysEnabled.Call(uintptr(unsafe.Pointer(s)), boolToBOOL(enabled))
	return hres(hr)
}

// Settings5.

func (s *ICoreWebView2Settings5) PutIsPinchZoomEnabled(enabled bool) error {
	hr, _, _ := s.Vtbl.PutIsPinchZoomEnabled.Call(uintptr(unsafe.Pointer(s)), boolToBOOL(enabled))
	return hres(hr)
}

// Settings9.

// PutIsNonClientRegionSupportEnabled turns on the app-region CSS style, which
// is the precondition for an HTML title bar that Windows treats as a real
// caption. Defaults to FALSE, and takes effect on the next navigation.
//
// Requires runtime 1.0.2420.47+; on anything older the QueryInterface for
// IIDICoreWebView2Settings9 fails and the caller should fall back to a native
// title bar.
func (s *ICoreWebView2Settings9) PutIsNonClientRegionSupportEnabled(enabled bool) error {
	hr, _, _ := s.Vtbl.PutIsNonClientRegionSupportEnabled.Call(uintptr(unsafe.Pointer(s)), boolToBOOL(enabled))
	return hres(hr)
}

func (s *ICoreWebView2Settings9) GetIsNonClientRegionSupportEnabled() (bool, error) {
	var enabled int32
	hr, _, _ := s.Vtbl.GetIsNonClientRegionSupportEnabled.Call(
		uintptr(unsafe.Pointer(s)),
		uintptr(unsafe.Pointer(&enabled)),
	)
	if err := hres(hr); err != nil {
		return false, err
	}
	return boolFromBOOL(enabled), nil
}

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
