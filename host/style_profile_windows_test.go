//go:build windows

package host

import "testing"

func TestStyleForNativeFrameProfile(t *testing.T) {
	tests := []struct {
		profile         nativeFrameProfile
		wantCaption     bool
		wantSysMenu     bool
		wantMinimizeBox bool
		wantMaximizeBox bool
	}{
		{profile: nativeFrameProfileBaseline, wantMinimizeBox: true, wantMaximizeBox: true},
		{profile: nativeFrameProfileNoCaptionDiagnostic, wantMinimizeBox: true, wantMaximizeBox: true},
		{profile: nativeFrameProfileSysMenu, wantSysMenu: true, wantMinimizeBox: true, wantMaximizeBox: true},
		{profile: nativeFrameProfileCaptionNCCalc, wantCaption: true, wantMinimizeBox: true, wantMaximizeBox: true},
		{profile: nativeFrameProfileCaptionButtonsDiag, wantCaption: true},
		{profile: nativeFrameProfileCaptionSnapDiag, wantCaption: true, wantMinimizeBox: true, wantMaximizeBox: true},
		{profile: nativeFrameProfileCaptionSysMenuNCCalc, wantCaption: true, wantSysMenu: true, wantMinimizeBox: true, wantMaximizeBox: true},
		{profile: nativeFrameProfileCaptionSysMenuNative, wantCaption: true, wantSysMenu: true, wantMinimizeBox: true, wantMaximizeBox: true},
	}
	for _, test := range tests {
		t.Run(string(test.profile), func(t *testing.T) {
			got := styleForNativeFrameProfile(test.profile, uintptr(wsOverlappedWindow))
			if (got&uintptr(wsCaption) != 0) != test.wantCaption {
				t.Fatalf("WS_CAPTION = %t, want %t", got&uintptr(wsCaption) != 0, test.wantCaption)
			}
			if (got&uintptr(wsSysMenu) != 0) != test.wantSysMenu {
				t.Fatalf("WS_SYSMENU = %t, want %t", got&uintptr(wsSysMenu) != 0, test.wantSysMenu)
			}
			if (got&uintptr(wsMinimizeBox) != 0) != test.wantMinimizeBox {
				t.Fatalf("WS_MINIMIZEBOX = %t, want %t", got&uintptr(wsMinimizeBox) != 0, test.wantMinimizeBox)
			}
			if (got&uintptr(wsMaximizeBox) != 0) != test.wantMaximizeBox {
				t.Fatalf("WS_MAXIMIZEBOX = %t, want %t", got&uintptr(wsMaximizeBox) != 0, test.wantMaximizeBox)
			}
			if !nativeFrameProfileMatchesStyle(test.profile, got) {
				t.Fatalf("nativeFrameProfileMatchesStyle(%q, 0x%x) = false", test.profile, got)
			}
		})
	}
}

func TestNativeFrameProfileRejectsMissingRequiredStyle(t *testing.T) {
	if nativeFrameProfileMatchesStyle(nativeFrameProfileBaseline, uintptr(wsThickFrame)) {
		t.Fatal("nativeFrameProfileMatchesStyle() accepted incomplete required style")
	}
}

func TestCaptionNCCalcUsesSnapCapableStyleBitsWithoutSysMenu(t *testing.T) {
	production := styleForNativeFrameProfile(nativeFrameProfileCaptionNCCalc, uintptr(wsOverlappedWindow))
	snapDiagnostic := styleForNativeFrameProfile(nativeFrameProfileCaptionSnapDiag, uintptr(wsOverlappedWindow))
	for _, bit := range []struct {
		name string
		mask uintptr
	}{
		{name: "WS_MINIMIZEBOX", mask: uintptr(wsMinimizeBox)},
		{name: "WS_MAXIMIZEBOX", mask: uintptr(wsMaximizeBox)},
	} {
		if production&bit.mask != snapDiagnostic&bit.mask {
			t.Fatalf("%s mismatch between caption_nccalc and caption_snap_diag", bit.name)
		}
	}
	if production&uintptr(wsCaption) == 0 {
		t.Fatal("caption_nccalc must keep WS_CAPTION for native Snap Layout caption-button routing")
	}
	if production&uintptr(wsSysMenu) != 0 {
		t.Fatal("caption_nccalc must keep WS_SYSMENU off")
	}
	if !nativeFrameProfileUsesDWMMaximizeCaptionButton(nativeFrameProfileCaptionNCCalc) {
		t.Fatal("caption_nccalc must route maximize caption-button messages through DWM")
	}
	if !nativeFrameProfileUsesMaximizeCaptionButtonHitTest(nativeFrameProfileCaptionNCCalc) {
		t.Fatal("caption_nccalc must expose only the maximize caption-button hit-test")
	}
}

func TestNativeFrameProfileHandlesNCCalcSizeOnlyForCalculatedRects(t *testing.T) {
	if nativeFrameProfileHandlesNCCalcSize(nativeFrameProfileCaptionNCCalc, 0) {
		t.Fatal("caption_nccalc must not consume WM_NCCALCSIZE with wParam=0")
	}
	if !nativeFrameProfileHandlesNCCalcSize(nativeFrameProfileCaptionNCCalc, 1) {
		t.Fatal("caption_nccalc must consume WM_NCCALCSIZE with wParam!=0")
	}
	if nativeFrameProfileHandlesNCCalcSize(nativeFrameProfileCaptionSysMenuNative, 1) {
		t.Fatal("caption_sysmenu_native_nccalc must use DefWindowProc NCCALCSIZE")
	}
}

func TestNativeFrameProfileExtendsClientArea(t *testing.T) {
	if !nativeFrameProfileExtendsClientArea(nativeFrameProfileCaptionNCCalc) {
		t.Fatal("caption_nccalc must keep the client extension")
	}
	if !nativeFrameProfileExtendsClientArea(nativeFrameProfileCaptionButtonsDiag) {
		t.Fatal("caption_buttons_diag must keep the client extension")
	}
	if !nativeFrameProfileExtendsClientArea(nativeFrameProfileCaptionSnapDiag) {
		t.Fatal("caption_snap_diag must keep the client extension")
	}
	if !nativeFrameProfileExtendsClientArea(nativeFrameProfileCaptionSysMenuNCCalc) {
		t.Fatal("caption_sysmenu_nccalc must keep the client extension")
	}
	if nativeFrameProfileExtendsClientArea(nativeFrameProfileCaptionSysMenuNative) {
		t.Fatal("caption_sysmenu_native_nccalc must use DefWindowProc")
	}
}
