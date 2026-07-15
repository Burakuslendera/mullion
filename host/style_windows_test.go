//go:build windows

package host

import (
	"strings"
	"testing"
)

func TestNativeFrameChangedFlagsPreserveWindowSize(t *testing.T) {
	for name, flag := range map[string]uintptr{
		"SWP_NOMOVE":       swpNoMove,
		"SWP_NOSIZE":       swpNoSize,
		"SWP_NOZORDER":     swpNoZOrder,
		"SWP_NOACTIVATE":   swpNoActivate,
		"SWP_FRAMECHANGED": swpFrameChanged,
	} {
		if nativeFrameChangedFlags&flag == 0 {
			t.Fatalf("nativeFrameChangedFlags missing %s", name)
		}
	}
}

func TestFormatNativeWindowStyleLog(t *testing.T) {
	got := formatNativeWindowStyleLog("after_framechanged", nativeWindowStyleAudit{
		style:              uintptr(wsCaption | wsSysMenu | wsThickFrame | wsMinimizeBox | wsMaximizeBox | wsVisible),
		exStyle:            0x100,
		cornerPreference:   dwmWindowCornerPreferenceRnd,
		cornerAvailable:    true,
		windowRect:         rect{Left: 10, Top: 20, Right: 910, Bottom: 640},
		windowRectValid:    true,
		clientRect:         rect{Left: 0, Top: 0, Right: 900, Bottom: 620},
		clientRectValid:    true,
		extendedFrame:      rect{Left: 8, Top: 18, Right: 912, Bottom: 642},
		extendedFrameValid: true,
	})
	for _, expected := range []string{
		"mullion: native style audit",
		"stage=after_framechanged",
		"ws_caption=true",
		"ws_sysmenu=true",
		"ws_thickframe=true",
		"ws_minimizebox=true",
		"ws_maximizebox=true",
		"ws_visible=true",
		"style=0x",
		"exstyle=0x100",
		"dwm_corner_preference=2",
		"window_rect=10:20:910:640",
		"client_rect=0:0:900:620",
		"extended_frame=8:18:912:642",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("formatNativeWindowStyleLog() missing %q:\n%s", expected, got)
		}
	}
}

func TestFormatNativeWindowStyleLogHandlesUnavailableDiagnostics(t *testing.T) {
	got := formatNativeWindowStyleLog("before", nativeWindowStyleAudit{style: uintptr(wsThickFrame)})
	for _, expected := range []string{
		"dwm_corner_preference=unavailable",
		"window_rect=unavailable",
		"client_rect=unavailable",
		"extended_frame=unavailable",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("formatNativeWindowStyleLog() missing %q:\n%s", expected, got)
		}
	}
}
