//go:build windows

package mullion

import "testing"

// The default profile is CaptionSysMenuNCCalc, i.e. CaptionNCCalc plus WS_SYSMENU.
// Dropping the style bit would silently disable right-click on the caption, a
// regression nothing else in the suite would catch.
func TestActiveNativeFrameProfileUsesTabStripProduction(t *testing.T) {
	if got := activeNativeFrameProfile(); got != nativeFrameProfileCaptionSysMenuNCCalc {
		t.Fatalf("activeNativeFrameProfile() = %q, want %q", got, nativeFrameProfileCaptionSysMenuNCCalc)
	}
}

func TestActiveNativeFrameProfileStyleBits(t *testing.T) {
	style := styleForNativeFrameProfile(activeNativeFrameProfile(), uintptr(wsOverlappedWindow))
	if style&uintptr(wsCaption) == 0 {
		t.Fatal("production profile must carry WS_CAPTION, style bit is unset")
	}
	if style&uintptr(wsSysMenu) == 0 {
		t.Fatal("production profile must carry WS_SYSMENU, style bit is unset (right-click system menu on the caption)")
	}
	if style&uintptr(wsMinimizeBox) == 0 {
		t.Fatal("production profile must carry WS_MINIMIZEBOX, style bit is unset")
	}
	if style&uintptr(wsMaximizeBox) == 0 {
		t.Fatal("production profile must carry WS_MAXIMIZEBOX, style bit is unset (Win+Z / edge Snap)")
	}
	if style&uintptr(wsThickFrame) == 0 {
		t.Fatal("production profile must carry WS_THICKFRAME, style bit is unset (resize)")
	}
}

// Frameless contract: the client area is extended over the frame, and the
// maximize-hover flyout paths stay off. Enabling either would put DWM back in
// charge of caption messages on a window that no longer has a native caption.
func TestActiveNativeFrameProfileFramelessNoHoverPaths(t *testing.T) {
	profile := activeNativeFrameProfile()
	if !nativeFrameProfileExtendsClientArea(profile) {
		t.Fatal("production profile must extend the client area over the frame, got false (frameless tab-strip)")
	}
	if !nativeFrameProfileHandlesNCCalcSize(profile, 1) {
		t.Fatal("production profile must handle WM_NCCALCSIZE with wParam=1, got false")
	}
	if nativeFrameProfileUsesMaximizeCaptionButtonHitTest(profile) {
		t.Fatal("production profile must not use the synthetic htMaxButton hit-test, got true")
	}
	if nativeFrameProfileUsesDWMMaximizeCaptionButton(profile) {
		t.Fatal("production profile must not forward the maximize caption button to DWM, got true")
	}
	if !nativeFrameProfileMatchesStyle(profile, styleForNativeFrameProfile(profile, uintptr(wsOverlappedWindow))) {
		t.Fatal("applied style must match the profile expectation, got no match")
	}
}
