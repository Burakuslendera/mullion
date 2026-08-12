//go:build windows

package host

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestNCCalcClientRectAddsRestoredFrameCompensation(t *testing.T) {
	target := rect{Left: 10, Top: 20, Right: 910, Bottom: 640}
	got := applyRestoredClientFrameCompensation(target)
	want := rect{Left: 10, Top: 20, Right: 910, Bottom: 641}
	if got != want {
		t.Fatalf("applyRestoredClientFrameCompensation() = %#v, want %#v", got, want)
	}
}

func TestNCCalcClientRectSaturatesRestoredFrameCompensationAtMaxCoordinate(t *testing.T) {
	const maxInt32 = int32(1<<31 - 1)
	target := rect{Left: 10, Top: 20, Right: 910, Bottom: maxInt32}
	if got := applyRestoredClientFrameCompensation(target); got != target {
		t.Fatalf("restored frame compensation overflowed rect: got %#v, want %#v", got, target)
	}
}

func TestNCCalcClientRectClampsMaximizedRectToWorkArea(t *testing.T) {
	target := rect{Left: -8, Top: -8, Right: 1930, Bottom: 1040}
	workArea := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1020}
	got, ok := clampRectToArea(target, workArea)
	if !ok {
		t.Fatal("clampRectToArea(maximized) ok = false")
	}
	if got != workArea {
		t.Fatalf("clampRectToArea(maximized) = %#v, want %#v", got, workArea)
	}
}

func TestNCCalcClientRectRejectsInvalidMaximizedClamp(t *testing.T) {
	target := rect{Left: 50, Top: 50, Right: 60, Bottom: 60}
	workArea := rect{Left: 100, Top: 100, Right: 120, Bottom: 120}
	if _, ok := clampRectToArea(target, workArea); ok {
		t.Fatal("clampRectToArea(invalid maximized) ok = true, want false")
	}
}

func TestNCCalcClientRectUsesDirectRectForFalse(t *testing.T) {
	host := &Host{nccalcIsZoomed: func(windowHandle) bool { return false }}
	value := rect{Left: 10, Top: 20, Right: 910, Bottom: 640}
	if got := host.windowProc(0, wmNCCalcSize, 0, uintptr(unsafe.Pointer(&value))); got != 0 {
		t.Fatalf("WM_NCCALCSIZE(FALSE) result = %#x, want 0", got)
	}
	want := rect{Left: 10, Top: 20, Right: 910, Bottom: 641}
	if value != want {
		t.Fatalf("WM_NCCALCSIZE(FALSE) rect = %#v, want %#v", value, want)
	}
}

func TestNCCalcClientRectUsesFirstCalculatedRectForTrue(t *testing.T) {
	host := &Host{nccalcIsZoomed: func(windowHandle) bool { return false }}
	params := ncCalcSizeParams{
		Rects: [3]rect{
			{Left: 10, Top: 20, Right: 910, Bottom: 640},
			{Left: 1, Top: 2, Right: 3, Bottom: 4},
			{Left: 5, Top: 6, Right: 7, Bottom: 8},
		},
		WindowPos: 0xFEED,
	}
	if got := host.windowProc(0, wmNCCalcSize, 1, uintptr(unsafe.Pointer(&params))); got != 0 {
		t.Fatalf("WM_NCCALCSIZE(TRUE) result = %#x, want 0", got)
	}
	want := rect{Left: 10, Top: 20, Right: 910, Bottom: 641}
	if params.Rects[0] != want {
		t.Fatalf("WM_NCCALCSIZE(TRUE) first rect = %#v, want %#v", params.Rects[0], want)
	}
	if params.Rects[1] != (rect{Left: 1, Top: 2, Right: 3, Bottom: 4}) ||
		params.Rects[2] != (rect{Left: 5, Top: 6, Right: 7, Bottom: 8}) ||
		params.WindowPos != 0xFEED {
		t.Fatalf("WM_NCCALCSIZE(TRUE) changed non-target bytes: %#v", params)
	}
}

// TestMaximizeGeometryUsesAutoHideInset runs the NCCALCSIZE path through the
// per-Host resolver seam and the real auto-hide geometry resolver. Low-level
// monitor and shell probes remain stubbed, so the test is headless while an
// ordinary raw work area cannot satisfy the expected reveal inset.
func TestMaximizeGeometryUsesAutoHideInset(t *testing.T) {
	monitor := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	rawWork := monitor // Auto-hide appbars do not reserve work-area pixels.
	want := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1079}

	originalInfo := monitorInfoForWindow
	originalEdges := autoHideEdgesForMonitor
	defer func() {
		monitorInfoForWindow = originalInfo
		autoHideEdgesForMonitor = originalEdges
	}()

	monitorCalls := 0
	monitorInfoForWindow = func(hwnd windowHandle) (monitorInfo, bool) {
		monitorCalls++
		return monitorInfo{Monitor: monitor, Work: rawWork}, true
	}
	edgeCalls := 0
	autoHideEdgesForMonitor = func(got rect) autoHideEdges {
		edgeCalls++
		if got != monitor {
			t.Fatalf("auto-hide probe monitor = %#v, want %#v", got, monitor)
		}
		return autoHideEdges{bottom: true}
	}

	zoomedCalls := 0
	resolverCalls := 0
	host := &Host{
		nccalcIsZoomed: func(windowHandle) bool {
			zoomedCalls++
			return true
		},
		nccalcMaximizeMonitorInfo: func(hwnd windowHandle) (monitorInfo, bool) {
			resolverCalls++
			return maximizeMonitorInfo(hwnd)
		},
	}
	pointerForms := []struct {
		name   string
		wParam uintptr
	}{
		{name: "direct rect", wParam: 0},
		{name: "calculation params", wParam: 1},
	}
	for _, form := range pointerForms {
		t.Run(form.name, func(t *testing.T) {
			value := rect{Left: -8, Top: -8, Right: 1930, Bottom: 1088}
			target := &value
			lParam := uintptr(unsafe.Pointer(target))
			var params ncCalcSizeParams
			if form.wParam != 0 {
				params = ncCalcSizeParams{Rects: [3]rect{value}}
				target = &params.Rects[0]
				lParam = uintptr(unsafe.Pointer(&params))
			}
			if got := host.windowProc(0, wmNCCalcSize, form.wParam, lParam); got != 0 {
				t.Fatalf("WM_NCCALCSIZE result = %#x, want 0", got)
			}
			if *target != want {
				t.Fatalf("WM_NCCALCSIZE rect = %#v, want auto-hide-inset work area %#v", *target, want)
			}
		})
	}
	if zoomedCalls != len(pointerForms) {
		t.Fatalf("NCCALC zoomed seam calls = %d, want %d", zoomedCalls, len(pointerForms))
	}
	if resolverCalls != len(pointerForms) {
		t.Fatalf("NCCALC maximize resolver seam calls = %d, want %d", resolverCalls, len(pointerForms))
	}
	if monitorCalls != len(pointerForms) || edgeCalls != len(pointerForms) {
		t.Fatalf("auto-hide geometry resolver monitor/edge calls = %d/%d, want %d/%d", monitorCalls, edgeCalls, len(pointerForms), len(pointerForms))
	}
}

func TestNCCalcFailurePolicyLogsAndPreservesFramelessClaim(t *testing.T) {

	tests := []struct {
		name    string
		monitor func(windowHandle) (monitorInfo, bool)
		reason  string
	}{
		{
			name:    "monitor unavailable",
			monitor: func(windowHandle) (monitorInfo, bool) { return monitorInfo{}, false },
			reason:  "monitor unavailable",
		},
		{
			name: "degenerate clamp",
			monitor: func(windowHandle) (monitorInfo, bool) {
				return monitorInfo{Work: rect{Left: 100, Top: 100, Right: 120, Bottom: 120}}, true
			},
			reason: "invalid monitor work area",
		},
	}
	pointerForms := []struct {
		name   string
		wParam uintptr
	}{
		{name: "direct rect", wParam: 0},
		{name: "calculation params", wParam: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, form := range pointerForms {
				t.Run(form.name, func(t *testing.T) {
					host, logger := newTestHost(t, Config{})
					host.nccalcIsZoomed = func(windowHandle) bool { return true }
					host.nccalcMaximizeMonitorInfo = test.monitor
					value := rect{Left: 50, Top: 50, Right: 60, Bottom: 60}
					target := &value
					lParam := uintptr(unsafe.Pointer(target))
					var params ncCalcSizeParams
					if form.wParam != 0 {
						params = ncCalcSizeParams{Rects: [3]rect{value}}
						target = &params.Rects[0]
						lParam = uintptr(unsafe.Pointer(&params))
					}
					if got := host.windowProc(0, wmNCCalcSize, form.wParam, lParam); got != 0 {
						t.Fatalf("WM_NCCALCSIZE result = %#x, want claimed result 0", got)
					}
					if want := (rect{Left: 50, Top: 50, Right: 60, Bottom: 60}); *target != want {
						t.Fatalf("degraded claim changed rect = %#v, want %#v", *target, want)
					}
					logText := logger.String()
					if !strings.Contains(logText, "nccalc client extension degraded, action=claim_unchanged, reason="+test.reason) {
						t.Fatalf("degraded claim log missing reason %q:\n%s", test.reason, logText)
					}
				})
			}
		})
	}
}

func TestNCCalcInvalidPointerLogsAndDelegates(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	const defaultResult = uintptr(0x125)
	var delegated []struct {
		hwnd    windowHandle
		message uint32
		wParam  uintptr
		lParam  uintptr
	}
	host.defaultWindowProc = func(hwnd windowHandle, message uint32, wParam, lParam uintptr) uintptr {
		delegated = append(delegated, struct {
			hwnd    windowHandle
			message uint32
			wParam  uintptr
			lParam  uintptr
		}{hwnd, message, wParam, lParam})
		return defaultResult
	}
	for _, wParam := range []uintptr{0, 1} {
		if got := host.windowProc(0, wmNCCalcSize, wParam, 0); got != defaultResult {
			t.Fatalf("WM_NCCALCSIZE invalid pointer result = %#x, want delegated result %#x", got, defaultResult)
		}
	}
	if want := []struct {
		hwnd    windowHandle
		message uint32
		wParam  uintptr
		lParam  uintptr
	}{
		{0, wmNCCalcSize, 0, 0},
		{0, wmNCCalcSize, 1, 0},
	}; !reflect.DeepEqual(delegated, want) {
		t.Fatalf("WM_NCCALCSIZE default delegation = %#v, want %#v", delegated, want)
	}
	logText := logger.String()
	if count := strings.Count(logText, "nccalc client extension degraded, action=delegate, reason=invalid client rect"); count != 2 {
		t.Fatalf("invalid pointer delegation logs = %d, want 2:\n%s", count, logText)
	}
}
