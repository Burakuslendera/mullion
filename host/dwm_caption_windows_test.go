//go:build windows

package host

import "testing"

func TestShouldUseDWMCaptionHitOnlyAcceptsHandledCaptionButtons(t *testing.T) {
	tests := []struct {
		name    string
		hit     uintptr
		handled bool
		want    bool
	}{
		{name: "unhandled max", hit: htMaxButton, handled: false, want: false},
		{name: "handled minimize", hit: htMinButton, handled: true, want: true},
		{name: "handled maximize", hit: htMaxButton, handled: true, want: true},
		{name: "handled close", hit: htClose, handled: true, want: true},
		{name: "handled caption ignored", hit: htCaption, handled: true, want: false},
		{name: "handled client ignored", hit: htClient, handled: true, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldUseDWMCaptionHit(test.hit, test.handled); got != test.want {
				t.Fatalf("shouldUseDWMCaptionHit() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestDWMCaptionProductionPolicyOnlyAcceptsMaximize(t *testing.T) {
	if got := shouldUseDWMCaptionHitForPolicy(htMinButton, true, nativeDWMCaptionPolicyMaximizeOnly); got {
		t.Fatal("maximize-only policy accepted HTMINBUTTON")
	}
	if got := shouldUseDWMCaptionHitForPolicy(htMaxButton, true, nativeDWMCaptionPolicyMaximizeOnly); !got {
		t.Fatal("maximize-only policy rejected HTMAXBUTTON")
	}
	if got := shouldUseDWMCaptionHitForPolicy(htClose, true, nativeDWMCaptionPolicyMaximizeOnly); got {
		t.Fatal("maximize-only policy accepted HTCLOSE")
	}
	if got := shouldUseDWMCaptionHitForPolicy(htMaxButton, false, nativeDWMCaptionPolicyMaximizeOnly); got {
		t.Fatal("maximize-only policy accepted unhandled HTMAXBUTTON")
	}
}

func TestNativeFrameProfileUsesDWMMaximizeCaptionButtonForSnapProfiles(t *testing.T) {
	if !nativeFrameProfileUsesDWMMaximizeCaptionButton(nativeFrameProfileCaptionNCCalc) {
		t.Fatal("caption_nccalc must use DWM maximize caption-button routing")
	}
	if !nativeFrameProfileUsesDWMMaximizeCaptionButton(nativeFrameProfileCaptionSnapDiag) {
		t.Fatal("caption_snap_diag must use DWM maximize caption-button routing")
	}
	if nativeFrameProfileUsesDWMMaximizeCaptionButton(nativeFrameProfileCaptionButtonsDiag) {
		t.Fatal("caption_buttons_diag must keep explicit project caption-button hit-test routing")
	}
}

func TestNativeFrameProfilesDoNotUseDynamicSnapCaptionByDefault(t *testing.T) {
	for _, profile := range []nativeFrameProfile{
		nativeFrameProfileCaptionNCCalc,
		nativeFrameProfileCaptionSnapDiag,
	} {
		if nativeFrameProfileUsesDynamicSnapCaption(profile) {
			t.Fatalf("%s must keep static caption style", profile)
		}
	}
}

func TestNativeCaptionMessageHitReadsSetCursorHitFromLParam(t *testing.T) {
	lParam := uintptr(htMaxButton) | (uintptr(wmNCMouseMove) << 16)
	if got := nativeCaptionMessageHit(wmSetCursor, htClient, lParam); got != htMaxButton {
		t.Fatalf("nativeCaptionMessageHit(WM_SETCURSOR) = %d, want HTMAXBUTTON", got)
	}
	if got := nativeCaptionMessageHit(wmNCMouseMove, htClose, 0); got != htClose {
		t.Fatalf("nativeCaptionMessageHit(WM_NCMOUSEMOVE) = %d, want HTCLOSE", got)
	}
}
