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
			got := shouldUseDWMCaptionHitForPolicy(test.hit, test.handled, nativeDWMCaptionPolicyAllButtons)
			if got != test.want {
				t.Fatalf("shouldUseDWMCaptionHitForPolicy(allButtons) = %t, want %t", got, test.want)
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

func TestNativeCaptionButtonHitNeededOnlyForReaders(t *testing.T) {
	tests := []struct {
		name                    string
		policy                  nativeDWMCaptionPolicy
		tooltipTraceReady       bool
		captionPassthroughReady bool
		want                    bool
	}{
		{name: "disabled without readers", policy: nativeDWMCaptionPolicyDisabled, want: false},
		{name: "disabled with trace", policy: nativeDWMCaptionPolicyDisabled, tooltipTraceReady: true, want: true},
		{name: "maximize policy without passthrough", policy: nativeDWMCaptionPolicyMaximizeOnly, want: false},
		{name: "maximize policy with passthrough", policy: nativeDWMCaptionPolicyMaximizeOnly, captionPassthroughReady: true, want: true},
		{name: "all-buttons policy without passthrough", policy: nativeDWMCaptionPolicyAllButtons, want: false},
		{name: "all-buttons policy with passthrough", policy: nativeDWMCaptionPolicyAllButtons, captionPassthroughReady: true, want: false},
		{name: "all-buttons policy with trace", policy: nativeDWMCaptionPolicyAllButtons, tooltipTraceReady: true, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nativeCaptionButtonHitNeeded(test.policy, test.tooltipTraceReady, test.captionPassthroughReady); got != test.want {
				t.Fatalf("nativeCaptionButtonHitNeeded(%v, trace=%t, passthrough=%t) = %t, want %t", test.policy, test.tooltipTraceReady, test.captionPassthroughReady, got, test.want)
			}
		})
	}
}

func TestNativeCaptionCandidateCompositionIsLazyAndReaderComplete(t *testing.T) {
	var calls int
	query := func(*Host, windowHandle, uintptr) uintptr {
		calls++
		return htMaxButton
	}
	const lParam = uintptr(0x1234)

	if got := nativeCaptionButtonHitIfNeeded(nil, 0, lParam, nativeDWMCaptionPolicyDisabled, false, false, query); got != htClient {
		t.Fatalf("unread caption candidate = %#x, want HTCLIENT sentinel", got)
	}
	if calls != 0 {
		t.Fatalf("unread caption candidate query calls = %d, want 0", calls)
	}

	for _, test := range []struct {
		name                    string
		policy                  nativeDWMCaptionPolicy
		tooltipTraceReady       bool
		captionPassthroughReady bool
	}{
		{name: "tooltip trace", policy: nativeDWMCaptionPolicyDisabled, tooltipTraceReady: true},
		{name: "caption passthrough", policy: nativeDWMCaptionPolicyMaximizeOnly, captionPassthroughReady: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nativeCaptionButtonHitIfNeeded(nil, 0, lParam, test.policy, test.tooltipTraceReady, test.captionPassthroughReady, query); got != htMaxButton {
				t.Fatalf("reader candidate = %#x, want HTMAXBUTTON", got)
			}
		})
	}
	if calls != 2 {
		t.Fatalf("reader caption candidate query calls = %d, want 2", calls)
	}
}

func TestNativeCaptionCandidateUnreadPathDoesNotAllocate(t *testing.T) {
	var sink uintptr
	allocs := testing.AllocsPerRun(1000, func() {
		sink = nativeCaptionButtonHitIfNeeded(nil, 0, 0, nativeDWMCaptionPolicyDisabled, false, false, nativeCaptionButtonHitForWindow)
	})
	if sink != htClient {
		t.Fatalf("unread caption candidate sink = %#x, want HTCLIENT sentinel", sink)
	}
	if allocs != 0 {
		t.Fatalf("unread caption candidate path allocations = %v, want 0", allocs)
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

func TestNativeCaptionMessageHitReadsSetCursorHitFromLParam(t *testing.T) {
	lParam := uintptr(htMaxButton) | (uintptr(wmNCMouseMove) << 16)
	if got := nativeCaptionMessageHit(wmSetCursor, htClient, lParam); got != htMaxButton {
		t.Fatalf("nativeCaptionMessageHit(WM_SETCURSOR) = %d, want HTMAXBUTTON", got)
	}
	if got := nativeCaptionMessageHit(wmNCMouseMove, htClose, 0); got != htClose {
		t.Fatalf("nativeCaptionMessageHit(WM_NCMOUSEMOVE) = %d, want HTCLOSE", got)
	}
}
