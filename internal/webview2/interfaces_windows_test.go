//go:build windows

package webview2

// ABI regression tests for the hand-written WebView2 COM vtables.
//
// These tests make no COM call and need no WebView2 runtime: they read struct
// layout with unsafe.Offsetof and compare it against the slot indices in
// Microsoft's WebView2.h (the MIDL-generated C ABI). That is the whole point.
// A wrong vtable offset never surfaces as a bad HRESULT - it dispatches a live
// call through a different function pointer, which corrupts memory or crashes
// inside embeddedbrowserwebview.dll, far from the mistake. Pinning the layout
// statically turns that into a test failure on a machine with no browser.
//
// Every slot of every interface is pinned, not just the ones this package
// calls, because an unused slot in the middle is exactly what keeps the used
// ones at the right offset.
//
// Source of truth: Microsoft.Web.WebView2 SDK, build/native/include/WebView2.h,
// interface *Vtbl structs.

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// slotSize is the width of one vtable entry: a function pointer.
const slotSize = unsafe.Sizeof(ComProc(0))

// slot is one pinned vtable entry: the field's measured offset and the slot
// index it must correspond to.
type slot struct {
	name   string
	offset uintptr
	index  uintptr
}

// checkVtbl asserts that a vtable is exactly wantSlots wide and that every
// listed field sits at its expected index.
func checkVtbl(t *testing.T, iface string, size uintptr, wantSlots uintptr, slots []slot) {
	t.Helper()
	if want := wantSlots * slotSize; size != want {
		t.Errorf("%s: vtbl size = %d bytes (%d slots), want %d bytes (%d slots)",
			iface, size, size/slotSize, want, wantSlots)
	}
	if got := uintptr(len(slots)); got != wantSlots {
		t.Errorf("%s: test pins %d slots, but the interface has %d - every slot must be pinned",
			iface, got, wantSlots)
	}
	for _, s := range slots {
		if want := s.index * slotSize; s.offset != want {
			t.Errorf("%s.%s: offset %d (slot %d), want offset %d (slot %d)",
				iface, s.name, s.offset, s.offset/slotSize, want, s.index)
		}
	}
}

func TestIStreamVtblLayout(t *testing.T) {
	var v IStreamVtbl
	// IUnknown(3) + ISequentialStream(2) + IStream(9) = 14.
	checkVtbl(t, "IStream", unsafe.Sizeof(v), 14, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"Read", unsafe.Offsetof(v.Read), 3},
		{"Write", unsafe.Offsetof(v.Write), 4},
		{"Seek", unsafe.Offsetof(v.Seek), 5},
		{"SetSize", unsafe.Offsetof(v.SetSize), 6},
		{"CopyTo", unsafe.Offsetof(v.CopyTo), 7},
		{"Commit", unsafe.Offsetof(v.Commit), 8},
		{"Revert", unsafe.Offsetof(v.Revert), 9},
		{"LockRegion", unsafe.Offsetof(v.LockRegion), 10},
		{"UnlockRegion", unsafe.Offsetof(v.UnlockRegion), 11},
		{"Stat", unsafe.Offsetof(v.Stat), 12},
		{"Clone", unsafe.Offsetof(v.Clone), 13},
	})
}

func TestEnvironmentVtblLayout(t *testing.T) {
	var v ICoreWebView2EnvironmentVtbl
	checkVtbl(t, "ICoreWebView2Environment", unsafe.Sizeof(v), 8, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"CreateCoreWebView2Controller", unsafe.Offsetof(v.CreateCoreWebView2Controller), 3},
		{"CreateWebResourceResponse", unsafe.Offsetof(v.CreateWebResourceResponse), 4},
		{"GetBrowserVersionString", unsafe.Offsetof(v.GetBrowserVersionString), 5},
		{"AddNewBrowserVersionAvailable", unsafe.Offsetof(v.AddNewBrowserVersionAvailable), 6},
		{"RemoveNewBrowserVersionAvailable", unsafe.Offsetof(v.RemoveNewBrowserVersionAvailable), 7},
	})
}

// controllerSlots is the ICoreWebView2Controller prefix, shared by Controller2
// and Controller3 through embedding. IsVisible precedes Bounds - the reverse
// order reads more naturally and is wrong.
func controllerSlots(v *ICoreWebView2ControllerVtbl) []slot {
	return []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"GetIsVisible", unsafe.Offsetof(v.GetIsVisible), 3},
		{"PutIsVisible", unsafe.Offsetof(v.PutIsVisible), 4},
		{"GetBounds", unsafe.Offsetof(v.GetBounds), 5},
		{"PutBounds", unsafe.Offsetof(v.PutBounds), 6},
		{"GetZoomFactor", unsafe.Offsetof(v.GetZoomFactor), 7},
		{"PutZoomFactor", unsafe.Offsetof(v.PutZoomFactor), 8},
		{"AddZoomFactorChanged", unsafe.Offsetof(v.AddZoomFactorChanged), 9},
		{"RemoveZoomFactorChanged", unsafe.Offsetof(v.RemoveZoomFactorChanged), 10},
		{"SetBoundsAndZoomFactor", unsafe.Offsetof(v.SetBoundsAndZoomFactor), 11},
		{"MoveFocus", unsafe.Offsetof(v.MoveFocus), 12},
		{"AddMoveFocusRequested", unsafe.Offsetof(v.AddMoveFocusRequested), 13},
		{"RemoveMoveFocusRequested", unsafe.Offsetof(v.RemoveMoveFocusRequested), 14},
		{"AddGotFocus", unsafe.Offsetof(v.AddGotFocus), 15},
		{"RemoveGotFocus", unsafe.Offsetof(v.RemoveGotFocus), 16},
		{"AddLostFocus", unsafe.Offsetof(v.AddLostFocus), 17},
		{"RemoveLostFocus", unsafe.Offsetof(v.RemoveLostFocus), 18},
		{"AddAcceleratorKeyPressed", unsafe.Offsetof(v.AddAcceleratorKeyPressed), 19},
		{"RemoveAcceleratorKeyPressed", unsafe.Offsetof(v.RemoveAcceleratorKeyPressed), 20},
		{"GetParentWindow", unsafe.Offsetof(v.GetParentWindow), 21},
		{"PutParentWindow", unsafe.Offsetof(v.PutParentWindow), 22},
		{"NotifyParentWindowPositionChanged", unsafe.Offsetof(v.NotifyParentWindowPositionChanged), 23},
		{"Close", unsafe.Offsetof(v.Close), 24},
		{"GetCoreWebView2", unsafe.Offsetof(v.GetCoreWebView2), 25},
	}
}

func TestControllerVtblLayout(t *testing.T) {
	var v ICoreWebView2ControllerVtbl
	checkVtbl(t, "ICoreWebView2Controller", unsafe.Sizeof(v), 26, controllerSlots(&v))
}

func TestController2VtblLayout(t *testing.T) {
	var v ICoreWebView2Controller2Vtbl
	slots := controllerSlots(&v.ICoreWebView2ControllerVtbl)
	slots = append(slots,
		slot{"GetDefaultBackgroundColor", unsafe.Offsetof(v.GetDefaultBackgroundColor), 26},
		slot{"PutDefaultBackgroundColor", unsafe.Offsetof(v.PutDefaultBackgroundColor), 27},
	)
	checkVtbl(t, "ICoreWebView2Controller2", unsafe.Sizeof(v), 28, slots)
}

func TestController3VtblLayout(t *testing.T) {
	var v ICoreWebView2Controller3Vtbl
	slots := controllerSlots(&v.ICoreWebView2ControllerVtbl)
	slots = append(slots,
		slot{"GetDefaultBackgroundColor", unsafe.Offsetof(v.GetDefaultBackgroundColor), 26},
		slot{"PutDefaultBackgroundColor", unsafe.Offsetof(v.PutDefaultBackgroundColor), 27},
		slot{"GetRasterizationScale", unsafe.Offsetof(v.GetRasterizationScale), 28},
		slot{"PutRasterizationScale", unsafe.Offsetof(v.PutRasterizationScale), 29},
		slot{"GetShouldDetectMonitorScaleChanges", unsafe.Offsetof(v.GetShouldDetectMonitorScaleChanges), 30},
		slot{"PutShouldDetectMonitorScaleChanges", unsafe.Offsetof(v.PutShouldDetectMonitorScaleChanges), 31},
		slot{"AddRasterizationScaleChanged", unsafe.Offsetof(v.AddRasterizationScaleChanged), 32},
		slot{"RemoveRasterizationScaleChanged", unsafe.Offsetof(v.RemoveRasterizationScaleChanged), 33},
		// BoundsMode is the tail of the interface. If the four
		// RasterizationScale/ShouldDetect slots above were collapsed (they look
		// like a pair, they are two pairs), BoundsMode would land on
		// add_RasterizationScaleChanged and PutBoundsMode would register an
		// event handler with the enum as a callback pointer.
		slot{"GetBoundsMode", unsafe.Offsetof(v.GetBoundsMode), 34},
		slot{"PutBoundsMode", unsafe.Offsetof(v.PutBoundsMode), 35},
	)
	checkVtbl(t, "ICoreWebView2Controller3", unsafe.Sizeof(v), 36, slots)
}

func TestCoreWebView2VtblLayout(t *testing.T) {
	var v ICoreWebView2Vtbl
	checkVtbl(t, "ICoreWebView2", unsafe.Sizeof(v), 61, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"GetSettings", unsafe.Offsetof(v.GetSettings), 3},
		{"GetSource", unsafe.Offsetof(v.GetSource), 4},
		{"Navigate", unsafe.Offsetof(v.Navigate), 5},
		{"NavigateToString", unsafe.Offsetof(v.NavigateToString), 6},
		{"AddNavigationStarting", unsafe.Offsetof(v.AddNavigationStarting), 7},
		{"RemoveNavigationStarting", unsafe.Offsetof(v.RemoveNavigationStarting), 8},
		{"AddContentLoading", unsafe.Offsetof(v.AddContentLoading), 9},
		{"RemoveContentLoading", unsafe.Offsetof(v.RemoveContentLoading), 10},
		{"AddSourceChanged", unsafe.Offsetof(v.AddSourceChanged), 11},
		{"RemoveSourceChanged", unsafe.Offsetof(v.RemoveSourceChanged), 12},
		{"AddHistoryChanged", unsafe.Offsetof(v.AddHistoryChanged), 13},
		{"RemoveHistoryChanged", unsafe.Offsetof(v.RemoveHistoryChanged), 14},
		{"AddNavigationCompleted", unsafe.Offsetof(v.AddNavigationCompleted), 15},
		{"RemoveNavigationCompleted", unsafe.Offsetof(v.RemoveNavigationCompleted), 16},
		{"AddFrameNavigationStarting", unsafe.Offsetof(v.AddFrameNavigationStarting), 17},
		{"RemoveFrameNavigationStarting", unsafe.Offsetof(v.RemoveFrameNavigationStarting), 18},
		{"AddFrameNavigationCompleted", unsafe.Offsetof(v.AddFrameNavigationCompleted), 19},
		{"RemoveFrameNavigationCompleted", unsafe.Offsetof(v.RemoveFrameNavigationCompleted), 20},
		{"AddScriptDialogOpening", unsafe.Offsetof(v.AddScriptDialogOpening), 21},
		{"RemoveScriptDialogOpening", unsafe.Offsetof(v.RemoveScriptDialogOpening), 22},
		{"AddPermissionRequested", unsafe.Offsetof(v.AddPermissionRequested), 23},
		{"RemovePermissionRequested", unsafe.Offsetof(v.RemovePermissionRequested), 24},
		{"AddProcessFailed", unsafe.Offsetof(v.AddProcessFailed), 25},
		{"RemoveProcessFailed", unsafe.Offsetof(v.RemoveProcessFailed), 26},
		{"AddScriptToExecuteOnDocumentCreated", unsafe.Offsetof(v.AddScriptToExecuteOnDocumentCreated), 27},
		{"RemoveScriptToExecuteOnDocumentCreated", unsafe.Offsetof(v.RemoveScriptToExecuteOnDocumentCreated), 28},
		{"ExecuteScript", unsafe.Offsetof(v.ExecuteScript), 29},
		{"CapturePreview", unsafe.Offsetof(v.CapturePreview), 30},
		{"Reload", unsafe.Offsetof(v.Reload), 31},
		{"PostWebMessageAsJson", unsafe.Offsetof(v.PostWebMessageAsJson), 32},
		{"PostWebMessageAsString", unsafe.Offsetof(v.PostWebMessageAsString), 33},
		{"AddWebMessageReceived", unsafe.Offsetof(v.AddWebMessageReceived), 34},
		{"RemoveWebMessageReceived", unsafe.Offsetof(v.RemoveWebMessageReceived), 35},
		{"CallDevToolsProtocolMethod", unsafe.Offsetof(v.CallDevToolsProtocolMethod), 36},
		{"GetBrowserProcessId", unsafe.Offsetof(v.GetBrowserProcessId), 37},
		{"GetCanGoBack", unsafe.Offsetof(v.GetCanGoBack), 38},
		{"GetCanGoForward", unsafe.Offsetof(v.GetCanGoForward), 39},
		{"GoBack", unsafe.Offsetof(v.GoBack), 40},
		{"GoForward", unsafe.Offsetof(v.GoForward), 41},
		{"GetDevToolsProtocolEventReceiver", unsafe.Offsetof(v.GetDevToolsProtocolEventReceiver), 42},
		{"Stop", unsafe.Offsetof(v.Stop), 43},
		{"AddNewWindowRequested", unsafe.Offsetof(v.AddNewWindowRequested), 44},
		{"RemoveNewWindowRequested", unsafe.Offsetof(v.RemoveNewWindowRequested), 45},
		{"AddDocumentTitleChanged", unsafe.Offsetof(v.AddDocumentTitleChanged), 46},
		{"RemoveDocumentTitleChanged", unsafe.Offsetof(v.RemoveDocumentTitleChanged), 47},
		{"GetDocumentTitle", unsafe.Offsetof(v.GetDocumentTitle), 48},
		{"AddHostObjectToScript", unsafe.Offsetof(v.AddHostObjectToScript), 49},
		{"RemoveHostObjectFromScript", unsafe.Offsetof(v.RemoveHostObjectFromScript), 50},
		{"OpenDevToolsWindow", unsafe.Offsetof(v.OpenDevToolsWindow), 51},
		{"AddContainsFullScreenElementChanged", unsafe.Offsetof(v.AddContainsFullScreenElementChanged), 52},
		{"RemoveContainsFullScreenElementChanged", unsafe.Offsetof(v.RemoveContainsFullScreenElementChanged), 53},
		{"GetContainsFullScreenElement", unsafe.Offsetof(v.GetContainsFullScreenElement), 54},
		{"AddWebResourceRequested", unsafe.Offsetof(v.AddWebResourceRequested), 55},
		{"RemoveWebResourceRequested", unsafe.Offsetof(v.RemoveWebResourceRequested), 56},
		// Slot 57 is the one this package depends on and the one furthest from
		// the front: it sits behind 30-plus event slots that are never called.
		{"AddWebResourceRequestedFilter", unsafe.Offsetof(v.AddWebResourceRequestedFilter), 57},
		{"RemoveWebResourceRequestedFilter", unsafe.Offsetof(v.RemoveWebResourceRequestedFilter), 58},
		{"AddWindowCloseRequested", unsafe.Offsetof(v.AddWindowCloseRequested), 59},
		{"RemoveWindowCloseRequested", unsafe.Offsetof(v.RemoveWindowCloseRequested), 60},
	})
}

func TestSettingsVtblLayout(t *testing.T) {
	var v ICoreWebView2SettingsVtbl
	checkVtbl(t, "ICoreWebView2Settings", unsafe.Sizeof(v), 21, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"GetIsScriptEnabled", unsafe.Offsetof(v.GetIsScriptEnabled), 3},
		{"PutIsScriptEnabled", unsafe.Offsetof(v.PutIsScriptEnabled), 4},
		{"GetIsWebMessageEnabled", unsafe.Offsetof(v.GetIsWebMessageEnabled), 5},
		{"PutIsWebMessageEnabled", unsafe.Offsetof(v.PutIsWebMessageEnabled), 6},
		{"GetAreDefaultScriptDialogsEnabled", unsafe.Offsetof(v.GetAreDefaultScriptDialogsEnabled), 7},
		{"PutAreDefaultScriptDialogsEnabled", unsafe.Offsetof(v.PutAreDefaultScriptDialogsEnabled), 8},
		{"GetIsStatusBarEnabled", unsafe.Offsetof(v.GetIsStatusBarEnabled), 9},
		{"PutIsStatusBarEnabled", unsafe.Offsetof(v.PutIsStatusBarEnabled), 10},
		{"GetAreDevToolsEnabled", unsafe.Offsetof(v.GetAreDevToolsEnabled), 11},
		{"PutAreDevToolsEnabled", unsafe.Offsetof(v.PutAreDevToolsEnabled), 12},
		{"GetAreDefaultContextMenusEnabled", unsafe.Offsetof(v.GetAreDefaultContextMenusEnabled), 13},
		{"PutAreDefaultContextMenusEnabled", unsafe.Offsetof(v.PutAreDefaultContextMenusEnabled), 14},
		{"GetAreHostObjectsAllowed", unsafe.Offsetof(v.GetAreHostObjectsAllowed), 15},
		{"PutAreHostObjectsAllowed", unsafe.Offsetof(v.PutAreHostObjectsAllowed), 16},
		{"GetIsZoomControlEnabled", unsafe.Offsetof(v.GetIsZoomControlEnabled), 17},
		{"PutIsZoomControlEnabled", unsafe.Offsetof(v.PutIsZoomControlEnabled), 18},
		{"GetIsBuiltInErrorPageEnabled", unsafe.Offsetof(v.GetIsBuiltInErrorPageEnabled), 19},
		{"PutIsBuiltInErrorPageEnabled", unsafe.Offsetof(v.PutIsBuiltInErrorPageEnabled), 20},
	})
}

// TestSettings9VtblLayout pins the full derived chain.
//
// The 39-slot budget is the anchor:
//
//	IUnknown(3) + base(18) + S2(2) + S3(2) + S4(4) + S5(2) + S6(2) + S7(2)
//	           + S8(2) + S9(2) = 39
//
// S4 contributing FOUR slots (IsPasswordAutosaveEnabled and
// IsGeneralAutofillEnabled) is the easiest one to get wrong; assuming the usual
// one-property-per-revision would put IsNonClientRegionSupportEnabled two slots
// early, on top of IsReputationCheckingRequired.
func TestSettings9VtblLayout(t *testing.T) {
	var v ICoreWebView2Settings9Vtbl
	base := &v.ICoreWebView2SettingsVtbl
	checkVtbl(t, "ICoreWebView2Settings9", unsafe.Sizeof(v), 39, []slot{
		{"QueryInterface", unsafe.Offsetof(base.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(base.AddRef), 1},
		{"Release", unsafe.Offsetof(base.Release), 2},
		{"GetIsScriptEnabled", unsafe.Offsetof(base.GetIsScriptEnabled), 3},
		{"PutIsScriptEnabled", unsafe.Offsetof(base.PutIsScriptEnabled), 4},
		{"GetIsWebMessageEnabled", unsafe.Offsetof(base.GetIsWebMessageEnabled), 5},
		{"PutIsWebMessageEnabled", unsafe.Offsetof(base.PutIsWebMessageEnabled), 6},
		{"GetAreDefaultScriptDialogsEnabled", unsafe.Offsetof(base.GetAreDefaultScriptDialogsEnabled), 7},
		{"PutAreDefaultScriptDialogsEnabled", unsafe.Offsetof(base.PutAreDefaultScriptDialogsEnabled), 8},
		{"GetIsStatusBarEnabled", unsafe.Offsetof(base.GetIsStatusBarEnabled), 9},
		{"PutIsStatusBarEnabled", unsafe.Offsetof(base.PutIsStatusBarEnabled), 10},
		{"GetAreDevToolsEnabled", unsafe.Offsetof(base.GetAreDevToolsEnabled), 11},
		{"PutAreDevToolsEnabled", unsafe.Offsetof(base.PutAreDevToolsEnabled), 12},
		{"GetAreDefaultContextMenusEnabled", unsafe.Offsetof(base.GetAreDefaultContextMenusEnabled), 13},
		{"PutAreDefaultContextMenusEnabled", unsafe.Offsetof(base.PutAreDefaultContextMenusEnabled), 14},
		{"GetAreHostObjectsAllowed", unsafe.Offsetof(base.GetAreHostObjectsAllowed), 15},
		{"PutAreHostObjectsAllowed", unsafe.Offsetof(base.PutAreHostObjectsAllowed), 16},
		{"GetIsZoomControlEnabled", unsafe.Offsetof(base.GetIsZoomControlEnabled), 17},
		{"PutIsZoomControlEnabled", unsafe.Offsetof(base.PutIsZoomControlEnabled), 18},
		{"GetIsBuiltInErrorPageEnabled", unsafe.Offsetof(base.GetIsBuiltInErrorPageEnabled), 19},
		{"PutIsBuiltInErrorPageEnabled", unsafe.Offsetof(base.PutIsBuiltInErrorPageEnabled), 20},
		// Settings2
		{"GetUserAgent", unsafe.Offsetof(v.GetUserAgent), 21},
		{"PutUserAgent", unsafe.Offsetof(v.PutUserAgent), 22},
		// Settings3
		{"GetAreBrowserAcceleratorKeysEnabled", unsafe.Offsetof(v.GetAreBrowserAcceleratorKeysEnabled), 23},
		{"PutAreBrowserAcceleratorKeysEnabled", unsafe.Offsetof(v.PutAreBrowserAcceleratorKeysEnabled), 24},
		// Settings4 - two properties, four slots
		{"GetIsPasswordAutosaveEnabled", unsafe.Offsetof(v.GetIsPasswordAutosaveEnabled), 25},
		{"PutIsPasswordAutosaveEnabled", unsafe.Offsetof(v.PutIsPasswordAutosaveEnabled), 26},
		{"GetIsGeneralAutofillEnabled", unsafe.Offsetof(v.GetIsGeneralAutofillEnabled), 27},
		{"PutIsGeneralAutofillEnabled", unsafe.Offsetof(v.PutIsGeneralAutofillEnabled), 28},
		// Settings5 is pinch zoom, Settings6 is swipe navigation - in that order
		{"GetIsPinchZoomEnabled", unsafe.Offsetof(v.GetIsPinchZoomEnabled), 29},
		{"PutIsPinchZoomEnabled", unsafe.Offsetof(v.PutIsPinchZoomEnabled), 30},
		{"GetIsSwipeNavigationEnabled", unsafe.Offsetof(v.GetIsSwipeNavigationEnabled), 31},
		{"PutIsSwipeNavigationEnabled", unsafe.Offsetof(v.PutIsSwipeNavigationEnabled), 32},
		// Settings7
		{"GetHiddenPdfToolbarItems", unsafe.Offsetof(v.GetHiddenPdfToolbarItems), 33},
		{"PutHiddenPdfToolbarItems", unsafe.Offsetof(v.PutHiddenPdfToolbarItems), 34},
		// Settings8
		{"GetIsReputationCheckingRequired", unsafe.Offsetof(v.GetIsReputationCheckingRequired), 35},
		{"PutIsReputationCheckingRequired", unsafe.Offsetof(v.PutIsReputationCheckingRequired), 36},
		// Settings9 - the anchor: 37 and 38
		{"GetIsNonClientRegionSupportEnabled", unsafe.Offsetof(v.GetIsNonClientRegionSupportEnabled), 37},
		{"PutIsNonClientRegionSupportEnabled", unsafe.Offsetof(v.PutIsNonClientRegionSupportEnabled), 38},
	})
}

// The intermediate Settings vtables must agree with the flattened Settings9
// layout, since a caller may QueryInterface to any of them. Settings3 and
// Settings5 are the two this package actually queries.
func TestSettingsChainSlotCounts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  uintptr
		slots uintptr
	}{
		{"ICoreWebView2Settings", unsafe.Sizeof(ICoreWebView2SettingsVtbl{}), 21},
		{"ICoreWebView2Settings2", unsafe.Sizeof(ICoreWebView2Settings2Vtbl{}), 23},
		{"ICoreWebView2Settings3", unsafe.Sizeof(ICoreWebView2Settings3Vtbl{}), 25},
		{"ICoreWebView2Settings4", unsafe.Sizeof(ICoreWebView2Settings4Vtbl{}), 29},
		{"ICoreWebView2Settings5", unsafe.Sizeof(ICoreWebView2Settings5Vtbl{}), 31},
		{"ICoreWebView2Settings6", unsafe.Sizeof(ICoreWebView2Settings6Vtbl{}), 33},
		{"ICoreWebView2Settings7", unsafe.Sizeof(ICoreWebView2Settings7Vtbl{}), 35},
		{"ICoreWebView2Settings8", unsafe.Sizeof(ICoreWebView2Settings8Vtbl{}), 37},
		{"ICoreWebView2Settings9", unsafe.Sizeof(ICoreWebView2Settings9Vtbl{}), 39},
	} {
		if want := tc.slots * slotSize; tc.size != want {
			t.Errorf("%s: vtbl = %d bytes (%d slots), want %d bytes (%d slots)",
				tc.name, tc.size, tc.size/slotSize, want, tc.slots)
		}
	}
}

// Settings3.PutAreBrowserAcceleratorKeysEnabled and
// Settings5.PutIsPinchZoomEnabled are called through their own QueryInterface'd
// pointers, so their offsets must match the position they occupy in the
// flattened chain (24 and 30). If these drifted, disabling Ctrl+R would instead
// write into some other property.
func TestSettingsQueriedInterfaceOffsets(t *testing.T) {
	var s3 ICoreWebView2Settings3Vtbl
	if got, want := unsafe.Offsetof(s3.PutAreBrowserAcceleratorKeysEnabled), 24*slotSize; got != want {
		t.Errorf("Settings3.PutAreBrowserAcceleratorKeysEnabled: offset %d (slot %d), want %d (slot 24)",
			got, got/slotSize, want)
	}
	var s5 ICoreWebView2Settings5Vtbl
	if got, want := unsafe.Offsetof(s5.PutIsPinchZoomEnabled), 30*slotSize; got != want {
		t.Errorf("Settings5.PutIsPinchZoomEnabled: offset %d (slot %d), want %d (slot 30)",
			got, got/slotSize, want)
	}
}

func TestWebResourceRequestVtblLayout(t *testing.T) {
	var v ICoreWebView2WebResourceRequestVtbl
	checkVtbl(t, "ICoreWebView2WebResourceRequest", unsafe.Sizeof(v), 10, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"GetUri", unsafe.Offsetof(v.GetUri), 3},
		{"PutUri", unsafe.Offsetof(v.PutUri), 4},
		{"GetMethod", unsafe.Offsetof(v.GetMethod), 5},
		{"PutMethod", unsafe.Offsetof(v.PutMethod), 6},
		{"GetContent", unsafe.Offsetof(v.GetContent), 7},
		{"PutContent", unsafe.Offsetof(v.PutContent), 8},
		{"GetHeaders", unsafe.Offsetof(v.GetHeaders), 9},
	})
}

func TestWebResourceRequestedEventArgsVtblLayout(t *testing.T) {
	var v ICoreWebView2WebResourceRequestedEventArgsVtbl
	checkVtbl(t, "ICoreWebView2WebResourceRequestedEventArgs", unsafe.Sizeof(v), 8, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"GetRequest", unsafe.Offsetof(v.GetRequest), 3},
		{"GetResponse", unsafe.Offsetof(v.GetResponse), 4},
		{"PutResponse", unsafe.Offsetof(v.PutResponse), 5},
		{"GetDeferral", unsafe.Offsetof(v.GetDeferral), 6},
		{"GetResourceContext", unsafe.Offsetof(v.GetResourceContext), 7},
	})
}

// Headers sits between Content and StatusCode. Grouping the members the way
// they are usually described - content, status, reason, headers - would put
// PutStatusCode on get_Headers, which takes an out-pointer: the runtime would
// write an interface pointer through the integer 200.
func TestWebResourceResponseVtblLayout(t *testing.T) {
	var v ICoreWebView2WebResourceResponseVtbl
	checkVtbl(t, "ICoreWebView2WebResourceResponse", unsafe.Sizeof(v), 10, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"GetContent", unsafe.Offsetof(v.GetContent), 3},
		{"PutContent", unsafe.Offsetof(v.PutContent), 4},
		{"GetHeaders", unsafe.Offsetof(v.GetHeaders), 5},
		{"GetStatusCode", unsafe.Offsetof(v.GetStatusCode), 6},
		{"PutStatusCode", unsafe.Offsetof(v.PutStatusCode), 7},
		{"GetReasonPhrase", unsafe.Offsetof(v.GetReasonPhrase), 8},
		{"PutReasonPhrase", unsafe.Offsetof(v.PutReasonPhrase), 9},
	})
}

func TestWebMessageReceivedEventArgsVtblLayout(t *testing.T) {
	var v ICoreWebView2WebMessageReceivedEventArgsVtbl
	checkVtbl(t, "ICoreWebView2WebMessageReceivedEventArgs", unsafe.Sizeof(v), 6, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"GetSource", unsafe.Offsetof(v.GetSource), 3},
		{"GetWebMessageAsJson", unsafe.Offsetof(v.GetWebMessageAsJson), 4},
		{"TryGetWebMessageAsString", unsafe.Offsetof(v.TryGetWebMessageAsString), 5},
	})
}

func TestNavigationStartingEventArgsVtblLayout(t *testing.T) {
	var v ICoreWebView2NavigationStartingEventArgsVtbl
	checkVtbl(t, "ICoreWebView2NavigationStartingEventArgs", unsafe.Sizeof(v), 10, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"GetUri", unsafe.Offsetof(v.GetUri), 3},
		{"GetIsUserInitiated", unsafe.Offsetof(v.GetIsUserInitiated), 4},
		{"GetIsRedirected", unsafe.Offsetof(v.GetIsRedirected), 5},
		{"GetRequestHeaders", unsafe.Offsetof(v.GetRequestHeaders), 6},
		{"GetCancel", unsafe.Offsetof(v.GetCancel), 7},
		{"PutCancel", unsafe.Offsetof(v.PutCancel), 8},
		{"GetNavigationID", unsafe.Offsetof(v.GetNavigationID), 9},
	})
}

func TestNavigationCompletedEventArgsVtblLayout(t *testing.T) {
	var v ICoreWebView2NavigationCompletedEventArgsVtbl
	checkVtbl(t, "ICoreWebView2NavigationCompletedEventArgs", unsafe.Sizeof(v), 6, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"GetIsSuccess", unsafe.Offsetof(v.GetIsSuccess), 3},
		{"GetWebErrorStatus", unsafe.Offsetof(v.GetWebErrorStatus), 4},
		{"GetNavigationID", unsafe.Offsetof(v.GetNavigationID), 5},
	})
}

func TestProcessFailedEventArgsVtblLayout(t *testing.T) {
	var v ICoreWebView2ProcessFailedEventArgsVtbl
	checkVtbl(t, "ICoreWebView2ProcessFailedEventArgs", unsafe.Sizeof(v), 4, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"GetProcessFailedKind", unsafe.Offsetof(v.GetProcessFailedKind), 3},
	})
}

// TestInterfaceIDs re-parses each IID from its canonical string form and
// compares it with the hand-transcribed windows.GUID literal. The literals are
// written out byte by byte into a Data4 array, so a single swapped nibble
// compiles cleanly and only shows up as a QueryInterface that returns
// E_NOINTERFACE - which the caller would misread as "old runtime, fall back".
func TestInterfaceIDs(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		got  windows.GUID
	}{
		{"ICoreWebView2Settings3", "{fdb5ab74-af33-4854-84f0-0a631deb5eba}", IIDICoreWebView2Settings3},
		{"ICoreWebView2Settings5", "{183e7052-1d03-43a0-ab99-98e043b66b39}", IIDICoreWebView2Settings5},
		{"ICoreWebView2Settings9", "{0528a73b-e92d-49f4-927a-e547dddaa37d}", IIDICoreWebView2Settings9},
		{"ICoreWebView2Controller2", "{c979903e-d4ca-4228-92eb-47ee3fa96eab}", IIDICoreWebView2Controller2},
		{"ICoreWebView2Controller3", "{f9614724-5d2b-41dc-aef7-73d62b51543b}", IIDICoreWebView2Controller3},
		// The event handler IIDs are what comServer answers QueryInterface with
		// when the runtime probes an object this package implements; a swapped
		// nibble there surfaces as add_* failing against a healthy runtime.
		{"ICoreWebView2WebMessageReceivedEventHandler", "{57213f19-00e6-49fa-8e07-898ea01ecbd2}", IIDICoreWebView2WebMessageReceivedEventHandler},
		{"ICoreWebView2WebResourceRequestedEventHandler", "{ab00b74c-15f1-4646-80e8-e76341d25d71}", IIDICoreWebView2WebResourceRequestedEventHandler},
		{"ICoreWebView2NavigationStartingEventHandler", "{9adbe429-f36d-432b-9ddc-f8881fbd76e3}", IIDICoreWebView2NavigationStartingEventHandler},
		{"ICoreWebView2NavigationCompletedEventHandler", "{d33a35bf-1c49-4f98-93ab-006e0533fe1c}", IIDICoreWebView2NavigationCompletedEventHandler},
		{"ICoreWebView2ProcessFailedEventHandler", "{79e0aea4-990b-42d9-aa1d-0fcc2e5bc7f1}", IIDICoreWebView2ProcessFailedEventHandler},
	} {
		want, err := windows.GUIDFromString(tc.text)
		if err != nil {
			t.Fatalf("%s: cannot parse reference IID %s: %v", tc.name, tc.text, err)
		}
		if tc.got != want {
			t.Errorf("%s: IID = %+v, want %+v (%s)", tc.name, tc.got, want, tc.text)
		}
	}
}

// Rect must be layout-identical to a Win32 RECT: four 32-bit fields, 16 bytes.
// The size is load-bearing beyond the field access - it is what makes the Win64
// ABI pass the struct by pointer rather than in a register, which is what
// PutBounds relies on.
func TestRectLayout(t *testing.T) {
	var r Rect
	if got := unsafe.Sizeof(r); got != 16 {
		t.Fatalf("Rect size = %d, want 16 (Win32 RECT)", got)
	}
	for _, tc := range []struct {
		name string
		off  uintptr
		want uintptr
	}{
		{"Left", unsafe.Offsetof(r.Left), 0},
		{"Top", unsafe.Offsetof(r.Top), 4},
		{"Right", unsafe.Offsetof(r.Right), 8},
		{"Bottom", unsafe.Offsetof(r.Bottom), 12},
	} {
		if tc.off != tc.want {
			t.Errorf("Rect.%s offset = %d, want %d", tc.name, tc.off, tc.want)
		}
	}
}

// TestColorPackMatchesMemoryLayout pins both halves of the COREWEBVIEW2_COLOR
// contract at once: the A,R,G,B field order, and the packing that PutDefaultBackgroundColor
// passes by value.
//
// Reinterpreting the struct as a uint32 is exactly what the Win64 ABI does with
// a 4-byte aggregate, so pack() agreeing with the raw memory is the definition
// of correct. If someone "fixes" the struct to R,G,B,A, this fails - whereas
// the call itself would keep working and just paint the wrong colour with the
// wrong opacity.
func TestColorPackMatchesMemoryLayout(t *testing.T) {
	if got := unsafe.Sizeof(Color{}); got != 4 {
		t.Fatalf("Color size = %d, want 4 (COREWEBVIEW2_COLOR is 4 BYTEs)", got)
	}
	for _, c := range []Color{
		{A: 255, R: 0, G: 0, B: 0},
		{A: 0, R: 255, G: 0, B: 0},
		{A: 0, R: 0, G: 255, B: 0},
		{A: 0, R: 0, G: 0, B: 255},
		{A: 0x12, R: 0x34, G: 0x56, B: 0x78},
	} {
		want := uintptr(*(*uint32)(unsafe.Pointer(&c)))
		if got := c.pack(); got != want {
			t.Errorf("Color%+v: pack() = %#x, want %#x (raw struct bytes)", c, got, want)
		}
	}
	// Spell out the field order independently of pack(), so that a matching
	// bug in both would still be caught.
	opaqueRed := Color{A: 0xFF, R: 0xFF, G: 0x00, B: 0x00}
	if got, want := opaqueRed.pack(), uintptr(0x0000FFFF); got != want {
		t.Errorf("opaque red packs to %#x, want %#x (A=0xFF lowest byte, R=0xFF next)", got, want)
	}
}

// EventRegistrationToken is written by the runtime through a pointer; if it
// were not 8 bytes the add_ methods would scribble past it.
func TestEventRegistrationTokenSize(t *testing.T) {
	if got := unsafe.Sizeof(EventRegistrationToken(0)); got != 8 {
		t.Fatalf("EventRegistrationToken size = %d, want 8 (__int64)", got)
	}
}

// The enum values are ABI, not policy: they are passed straight through to the
// runtime. BoundsModeUseRawPixels being 0 is what lets this host feed WebView2
// physical pixels, and WebResourceContextAll being 0 is what makes the resource
// filter match every request rather than only documents.
func TestEnumValues(t *testing.T) {
	if BoundsModeUseRawPixels != 0 {
		t.Errorf("COREWEBVIEW2_BOUNDS_MODE_USE_RAW_PIXELS = %d, want 0", BoundsModeUseRawPixels)
	}
	if BoundsModeUseRasterizationScale != 1 {
		t.Errorf("COREWEBVIEW2_BOUNDS_MODE_USE_RASTERIZATION_SCALE = %d, want 1", BoundsModeUseRasterizationScale)
	}
	if WebResourceContextAll != 0 {
		t.Errorf("COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL = %d, want 0", WebResourceContextAll)
	}
	if ProcessFailedKindBrowserProcessExited != 0 {
		t.Errorf("COREWEBVIEW2_PROCESS_FAILED_KIND_BROWSER_PROCESS_EXITED = %d, want 0", ProcessFailedKindBrowserProcessExited)
	}
	if ProcessFailedKindRenderProcessExited != 1 {
		t.Errorf("COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED = %d, want 1", ProcessFailedKindRenderProcessExited)
	}
}

func TestBoolConversions(t *testing.T) {
	if got := boolToBOOL(true); got != 1 {
		t.Errorf("boolToBOOL(true) = %d, want 1", got)
	}
	if got := boolToBOOL(false); got != 0 {
		t.Errorf("boolToBOOL(false) = %d, want 0", got)
	}
	// Win32 TRUE is "non-zero", not "== 1": a runtime that reports 2 or -1 must
	// still read as true.
	for _, v := range []int32{1, 2, -1} {
		if !boolFromBOOL(v) {
			t.Errorf("boolFromBOOL(%d) = false, want true (BOOL is non-zero, not ==1)", v)
		}
	}
	if boolFromBOOL(0) {
		t.Error("boolFromBOOL(0) = true, want false")
	}
}

// takeWstr must tolerate a nil LPWSTR. The runtime is allowed to hand back S_OK
// with a null string (an empty header, say), and a nil deref here would crash
// inside an event handler.
func TestTakeWstrNil(t *testing.T) {
	if got := takeWstr(nil); got != "" {
		t.Errorf("takeWstr(nil) = %q, want empty string", got)
	}
}
