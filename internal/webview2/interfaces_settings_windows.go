//go:build windows

package webview2

// The ICoreWebView2Settings chain. Split from interfaces_windows.go, whose
// header carries the ABI contract that governs every vtable struct here.

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Transcribed from the MIDL_INTERFACE attributes in WebView2.h. A single
// swapped nibble compiles fine and only shows up as a QueryInterface miss at
// runtime, so interfaces_windows_test.go re-parses each one from its canonical
// string form and compares.
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
)

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
