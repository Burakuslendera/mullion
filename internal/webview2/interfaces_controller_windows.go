//go:build windows

package webview2

// The ICoreWebView2Controller chain. Split from interfaces_windows.go, whose
// header carries the ABI contract that governs every vtable struct here.

import (
	"math"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Transcribed from the MIDL_INTERFACE attributes in WebView2.h. A single
// swapped nibble compiles fine and only shows up as a QueryInterface miss at
// runtime, so interfaces_windows_test.go re-parses each one from its canonical
// string form and compares.
var (
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
