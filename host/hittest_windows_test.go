//go:build windows

package host

import "testing"

// testMetrics is the default frame geometry. The hit-test maths is pure, so the
// tests drive it directly with the metrics a default Config produces.
var testMetrics = Config{}.normalise().hitTestMetrics()

func TestHitTestResizeBorder(t *testing.T) {
	windowRect := rect{Left: -100, Top: 50, Right: 900, Bottom: 650}
	tests := []struct {
		name   string
		cursor point
		want   int32
	}{
		{name: "top left", cursor: point{X: -100, Y: 50}, want: htTopLeft},
		{name: "top right", cursor: point{X: 899, Y: 50}, want: htTopRight},
		{name: "bottom left", cursor: point{X: -100, Y: 649}, want: htBottomLeft},
		{name: "bottom right", cursor: point{X: 899, Y: 649}, want: htBottomRight},
		{name: "top", cursor: point{X: 100, Y: 50}, want: htTop},
		{name: "client", cursor: point{X: 100, Y: 100}, want: htClient},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hitTestResizeBorder(windowRect, test.cursor, 8); got != test.want {
				t.Fatalf("hitTestResizeBorder() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestScaleLogicalPixels(t *testing.T) {
	tests := []struct {
		dpi  uint32
		want int32
	}{
		{dpi: 96, want: 8},
		{dpi: 120, want: 10},
		{dpi: 144, want: 12},
		{dpi: 192, want: 16},
	}
	for _, test := range tests {
		if got := scaleLogicalPixels(8, test.dpi); got != test.want {
			t.Fatalf("scaleLogicalPixels(8, %d) = %d, want %d", test.dpi, got, test.want)
		}
	}
}

func TestPointLParamRoundTripKeepsSignedCoordinates(t *testing.T) {
	tests := []point{
		{X: 120, Y: 240},
		{X: -12, Y: 40},
		{X: 40, Y: -18},
		{X: -800, Y: -120},
	}
	for _, test := range tests {
		if got := pointFromLParam(pointToLParam(test)); got != test {
			t.Fatalf("point round trip = %#v, want %#v", got, test)
		}
	}
}

func TestNativeHitTestForRectKeepsControlsClientSide(t *testing.T) {
	windowRect := rect{Left: 100, Top: 100, Right: 1000, Bottom: 720}
	controlsWidth := scaleLogicalPixels(testMetrics.ControlsWidth, 96)
	buttonWidth := controlsWidth / 3
	left := windowRect.Right - controlsWidth
	wantControls := []int32{htClient, htClient, htClient}
	if nativeFrameProfileUsesCaptionButtonHitTest(activeNativeFrameProfile()) {
		wantControls = []int32{htMinButton, htMaxButton, htClose}
	} else if nativeFrameProfileUsesMaximizeCaptionButtonHitTest(activeNativeFrameProfile()) {
		wantControls = []int32{htClient, htMaxButton, htClient}
	}
	for index, cursorX := range []int32{
		left + buttonWidth/2,
		left + buttonWidth + buttonWidth/2,
		left + 2*buttonWidth + buttonWidth/2,
	} {
		if got := nativeHitTestForRect(testMetrics, windowRect, point{X: cursorX, Y: 116}, 96, false); got != wantControls[index] {
			t.Fatalf("nativeHitTestForRect(testMetrics, control=%d) = %d, want %d", index, got, wantControls[index])
		}
	}
	if got := nativeHitTestForRect(testMetrics, windowRect, point{X: 500, Y: 116}, 96, false); got != htCaption {
		t.Fatalf("nativeHitTestForRect(testMetrics, ) = %d, want HTCAPTION", got)
	}
}

func TestNativeCaptionButtonHitForRectIdentifiesControlsWithoutChangingProjectHitTest(t *testing.T) {
	windowRect := rect{Left: 100, Top: 100, Right: 1000, Bottom: 720}
	controlsWidth := scaleLogicalPixels(testMetrics.ControlsWidth, 96)
	buttonWidth := controlsWidth / 3
	left := windowRect.Right - controlsWidth
	titlebarY := windowRect.Top + scaleLogicalPixels(testMetrics.ResizeBorder, 96)
	tests := []struct {
		name string
		x    int32
		want int32
	}{
		{name: "minimize", x: left + buttonWidth/2, want: htMinButton},
		{name: "maximize", x: left + buttonWidth + buttonWidth/2, want: htMaxButton},
		{name: "close", x: left + 2*buttonWidth + buttonWidth/2, want: htClose},
	}
	for _, test := range tests {
		cursor := point{X: test.x, Y: titlebarY}
		if got := nativeCaptionButtonHitForRect(testMetrics, windowRect, cursor, 96, false); got != test.want {
			t.Fatalf("nativeCaptionButtonHitForRect(testMetrics, %s) = %d, want %d", test.name, got, test.want)
		}
		wantProjectHit := int32(htClient)
		if test.want == htMaxButton && nativeFrameProfileUsesMaximizeCaptionButtonHitTest(activeNativeFrameProfile()) {
			wantProjectHit = htMaxButton
		}
		if got := nativeHitTestForRect(testMetrics, windowRect, cursor, 96, false); got != wantProjectHit {
			t.Fatalf("nativeHitTestForRect(testMetrics, %s) = %d, want %d", test.name, got, wantProjectHit)
		}
	}
}

func TestNativeHitTestForRectSeparatesTopResizeFromTitlebarDrag(t *testing.T) {
	windowRect := rect{Left: 100, Top: 100, Right: 1000, Bottom: 720}
	for _, dpi := range []uint32{96, 120, 144} {
		border := scaleLogicalPixels(testMetrics.ResizeBorder, dpi)
		titlebarHeight := scaleLogicalPixels(testMetrics.TitlebarHeight, dpi)
		tests := []struct {
			name string
			y    int32
			want int32
		}{
			{name: "top edge first pixel", y: windowRect.Top, want: htTop},
			{name: "top edge last pixel", y: windowRect.Top + border - 1, want: htTop},
			{name: "caption first pixel", y: windowRect.Top + border, want: htCaption},
			{name: "caption last pixel", y: windowRect.Top + titlebarHeight - 1, want: htCaption},
			{name: "client after titlebar", y: windowRect.Top + titlebarHeight, want: htClient},
		}
		for _, test := range tests {
			if got := nativeHitTestForRect(testMetrics, windowRect, point{X: 500, Y: test.y}, dpi, false); got != test.want {
				t.Fatalf("nativeHitTestForRect(testMetrics, %s, dpi=%d, y=%d) = %d, want %d", test.name, dpi, test.y, got, test.want)
			}
		}
	}
}

func TestNativeHitTestForRectSkipsResizeBorderWhenMaximized(t *testing.T) {
	windowRect := rect{Left: 100, Top: 100, Right: 1000, Bottom: 720}
	for _, dpi := range []uint32{96, 120, 144} {
		titlebarHeight := scaleLogicalPixels(testMetrics.TitlebarHeight, dpi)
		if got := nativeHitTestForRect(testMetrics, windowRect, point{X: 500, Y: windowRect.Top}, dpi, true); got != htCaption {
			t.Fatalf("nativeHitTestForRect(testMetrics, maximized titlebar, dpi=%d) = %d, want HTCAPTION", dpi, got)
		}
		if got := nativeHitTestForRect(testMetrics, windowRect, point{X: 500, Y: windowRect.Top + titlebarHeight}, dpi, true); got != htClient {
			t.Fatalf("nativeHitTestForRect(testMetrics, maximized client, dpi=%d) = %d, want HTCLIENT", dpi, got)
		}
		wantControls := int32(htClient)
		if nativeFrameProfileUsesCaptionButtonHitTest(activeNativeFrameProfile()) {
			wantControls = htClose
		}
		if got := nativeHitTestForRect(testMetrics, windowRect, point{X: 980, Y: windowRect.Top}, dpi, true); got != wantControls {
			t.Fatalf("nativeHitTestForRect(testMetrics, maximized controls, dpi=%d) = %d, want %d", dpi, got, wantControls)
		}
	}
}

func TestMaximizedHitTestRectClampsToWorkAreaWithoutEatingTopBand(t *testing.T) {
	workArea := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1020}
	windowRect := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1020}
	got, ok := maximizedHitTestRectForWorkArea(windowRect, workArea)
	if !ok {
		t.Fatal("maximizedHitTestRectForWorkArea() ok = false")
	}
	if got.Top != workArea.Top {
		t.Fatalf("maximized top = %d, want %d", got.Top, workArea.Top)
	}
	if hit := nativeHitTestForRect(testMetrics, got, point{X: 500, Y: workArea.Top}, 120, true); hit != htCaption {
		t.Fatalf("maximized top band hit = %d, want HTCAPTION", hit)
	}

	extendedRect := rect{Left: -10, Top: -10, Right: 1930, Bottom: 1030}
	got, ok = maximizedHitTestRectForWorkArea(extendedRect, workArea)
	if !ok {
		t.Fatal("maximizedHitTestRectForWorkArea(extended) ok = false")
	}
	if got != workArea {
		t.Fatalf("maximized extended rect = %#v, want %#v", got, workArea)
	}
}

// TestWindowRectForMaximizedHitTestStaysInProcess locks the routing fixed by issue
// #36 (docs/decisions/0019): the maximized hit-test rect is derived from
// monitorInfoForWindow's un-inset work area and never probes the shell for auto-hide
// edges. Re-routing it through maximizeMonitorInfo - the exact "consistency" cleanup
// 0015's wording used to invite - trips both assertions: the seam counter sees the
// SHAppBarMessage probe, and the inset work area shifts the clamped rect off the
// expected value. The monitor seam succeeds headlessly, so the probe path is
// reachable on any machine and the counter is a deterministic zero, not an artefact
// of a missing display (decision 0006).
func TestWindowRectForMaximizedHitTestStaysInProcess(t *testing.T) {
	monitor := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	work := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1040}

	origInfo := monitorInfoForWindow
	origEdges := autoHideEdgesForMonitor
	defer func() {
		monitorInfoForWindow = origInfo
		autoHideEdgesForMonitor = origEdges
	}()

	monitorInfoForWindow = func(windowHandle) (monitorInfo, bool) {
		return monitorInfo{Monitor: monitor, Work: work}, true
	}
	shellProbes := 0
	autoHideEdgesForMonitor = func(rect) autoHideEdges {
		shellProbes++
		return autoHideEdges{bottom: true}
	}

	// A frame-overhang rect clamps to the full, un-inset work area. Routed through
	// maximizeMonitorInfo this would come back with Bottom=1039 instead.
	if got := windowRectForMaximizedHitTest(0, rect{Left: -8, Top: -8, Right: 1928, Bottom: 1048}); got != work {
		t.Errorf("overhanging rect = %#v, want un-inset work area %#v", got, work)
	}

	// A window already sized to an auto-hide-inset work area (WM_GETMINMAXINFO did
	// the inset - decision 0015) passes through unchanged: the min/max clamp must
	// not undo the reveal sliver.
	inset := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1039}
	if got := windowRectForMaximizedHitTest(0, inset); got != inset {
		t.Errorf("inset rect = %#v, want unchanged %#v", got, inset)
	}

	if shellProbes != 0 {
		t.Errorf("shell probed %d times on the hit-test path, want 0 (issue #36)", shellProbes)
	}

	// A failed monitor query falls back to the raw window rect - and still no probe.
	monitorInfoForWindow = func(windowHandle) (monitorInfo, bool) { return monitorInfo{}, false }
	raw := rect{Left: 3, Top: 4, Right: 500, Bottom: 600}
	if got := windowRectForMaximizedHitTest(0, raw); got != raw {
		t.Errorf("failed monitor query rect = %#v, want raw %#v", got, raw)
	}
	if shellProbes != 0 {
		t.Errorf("shell probed %d times on failed monitor query, want 0", shellProbes)
	}
}

func TestResizeHitTestForEdge(t *testing.T) {
	tests := map[string]int32{
		"left":         htLeft,
		"right":        htRight,
		"top":          htTop,
		"bottom":       htBottom,
		"top-left":     htTopLeft,
		"top-right":    htTopRight,
		"bottom-left":  htBottomLeft,
		"bottom-right": htBottomRight,
	}
	for edge, want := range tests {
		got, ok := resizeHitTestForEdge(edge)
		if !ok || got != want {
			t.Fatalf("resizeHitTestForEdge(%q) = %d, %t; want %d, true", edge, got, ok, want)
		}
	}
	if _, ok := resizeHitTestForEdge("center"); ok {
		t.Fatal("resizeHitTestForEdge(center) ok = true, want false")
	}
}

func TestResizeFallbackPointMapsToResizeHit(t *testing.T) {
	windowRect := rect{Left: 100, Top: 100, Right: 1000, Bottom: 720}
	tests := []int32{
		htLeft,
		htRight,
		htTop,
		htBottom,
		htTopLeft,
		htTopRight,
		htBottomLeft,
		htBottomRight,
	}
	for _, hit := range tests {
		cursor, ok := resizeFallbackPoint(windowRect, hit)
		if !ok {
			t.Fatalf("resizeFallbackPoint(%d) ok = false, want true", hit)
		}
		if got := hitTestResizeBorder(windowRect, cursor, 8); got != hit {
			t.Fatalf("fallback hit = %d, want %d for cursor %#v", got, hit, cursor)
		}
	}
	if _, ok := resizeFallbackPoint(windowRect, htClient); ok {
		t.Fatal("resizeFallbackPoint(HTCLIENT) ok = true, want false")
	}
}
