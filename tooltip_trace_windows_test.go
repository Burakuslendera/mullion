//go:build windows

package mullion

import (
	"strings"
	"testing"
)

func TestFormatNativeTooltipMessage(t *testing.T) {
	if got := formatNativeTooltipMessage(wmNCMouseLeave); got != "WM_NCMOUSELEAVE" {
		t.Fatalf("formatNativeTooltipMessage() = %q", got)
	}
	if got := formatNativeTooltipMessage(0x1234); got != "0x1234" {
		t.Fatalf("formatNativeTooltipMessage() = %q", got)
	}
}

func TestFormatNativeTooltipHit(t *testing.T) {
	if got := formatNativeTooltipHit(htMaxButton); got != "HTMAXBUTTON" {
		t.Fatalf("formatNativeTooltipHit() = %q", got)
	}
	if got := formatNativeTooltipHit(77); got != "77" {
		t.Fatalf("formatNativeTooltipHit() = %q", got)
	}
}

func TestShouldTrackNativeTooltipMouse(t *testing.T) {
	if !shouldTrackNativeTooltipMouse(htCaption, htClient, false) {
		t.Fatal("caption project hit should enable non-client tracking")
	}
	if !shouldTrackNativeTooltipMouse(htClient, htMaxButton, true) {
		t.Fatal("DWM max button hit should enable non-client tracking")
	}
	if shouldTrackNativeTooltipMouse(htClient, htClient, true) {
		t.Fatal("client hit should not enable non-client tracking")
	}
}

func TestShouldRetrackNativeTooltipMessage(t *testing.T) {
	if !shouldRetrackNativeTooltipMessage(wmNCMouseHover) {
		t.Fatal("WM_NCMOUSEHOVER should re-arm non-client tracking")
	}
	if shouldRetrackNativeTooltipMessage(wmNCMouseLeave) {
		t.Fatal("WM_NCMOUSELEAVE should not re-arm non-client tracking")
	}
	if shouldRetrackNativeTooltipMessage(wmNCMouseMove) {
		t.Fatal("WM_NCMOUSEMOVE should not re-arm non-client tracking")
	}
}

func TestFormatNativeTooltipStyleBitsIncludesSnapRelevantFlags(t *testing.T) {
	got := formatNativeTooltipStyleBits(uintptr(wsCaption | wsThickFrame | wsMinimizeBox | wsMaximizeBox))
	for _, want := range []string{
		"ws_caption=true",
		"ws_sysmenu=false",
		"ws_thickframe=true",
		"ws_minimizebox=true",
		"ws_maximizebox=true",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatNativeTooltipStyleBits() missing %q in %q", want, got)
		}
	}
}

func TestFormatNativeTooltipWindowFromPointClassContext(t *testing.T) {
	got := formatNativeTooltipWindowFromPointClassContext("Chrome_WidgetWin_0", false, true)
	for _, want := range []string{
		"hover_hwnd_class=Chrome_WidgetWin_0",
		"hover_hwnd_is_parent=false",
		"hover_hwnd_is_child=true",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatNativeTooltipWindowFromPointClassContext() missing %q in %q", want, got)
		}
	}
}

func TestFormatNativeCaptionRoute(t *testing.T) {
	if got := formatNativeCaptionRoute(nativeCaptionRouteDWM); got != "dwm" {
		t.Fatalf("formatNativeCaptionRoute() = %q", got)
	}
}
