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
			geometry, ok := newHitTestGeometry(hitTestMetrics{ResizeBorder: 8}, windowRect, test.cursor, 96)
			if !ok {
				t.Fatal("newHitTestGeometry() ok = false")
			}
			if got := hitTestResizeBorder(geometry); got != test.want {
				t.Fatalf("hitTestResizeBorder() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestScaleLogicalPixels(t *testing.T) {
	tests := []struct {
		name string
		px   int32
		dpi  uint32
		want int64
	}{
		{name: "negative is empty", px: -8, dpi: 144, want: 0},
		{name: "zero is empty", px: 0, dpi: 144, want: 0},
		{name: "zero DPI uses 96", px: 8, dpi: 0, want: 8},
		{name: "exact", px: 8, dpi: 120, want: 10},
		{name: "ceiling", px: 1, dpi: 97, want: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := scaleLogicalPixels(test.px, test.dpi); got != test.want {
				t.Fatalf("scaleLogicalPixels(%d, %d) = %d, want %d", test.px, test.dpi, got, test.want)
			}
		})
	}

	const maxInt32 = int32(1<<31 - 1)
	const maxUint32 = ^uint32(0)
	product := int64(maxInt32) * int64(maxUint32)
	want := product / int64(defaultWindowDPI)
	if product%int64(defaultWindowDPI) != 0 {
		want++
	}
	if got := scaleLogicalPixels(maxInt32, maxUint32); got != want {
		t.Fatalf("scaleLogicalPixels(MaxInt32, MaxUint32) = %d, want exact ceiling %d", got, want)
	}
}

func TestNativeResolversKeepSeededLargeTitleBands(t *testing.T) {
	const maxInt32 = int32(1<<31 - 1)
	tests := []struct {
		name       string
		metrics    hitTestMetrics
		windowRect rect
		cursor     point
		dpi        uint32
	}{
		{
			name:       "one point five billion at 96 DPI",
			metrics:    hitTestMetrics{TitlebarHeight: 1_500_000_000},
			windowRect: rect{Left: 0, Top: 0, Right: 1000, Bottom: 600},
			cursor:     point{X: 500, Y: 550},
			dpi:        96,
		},
		{
			name:       "one point five billion at 192 DPI",
			metrics:    hitTestMetrics{TitlebarHeight: 1_500_000_000},
			windowRect: rect{Left: 0, Top: 0, Right: 1000, Bottom: 600},
			cursor:     point{X: 500, Y: 550},
			dpi:        192,
		},
		{
			name:       "positive int32 wrap seed at 240 DPI",
			metrics:    hitTestMetrics{TitlebarHeight: 1_717_987_118},
			windowRect: rect{Left: 0, Top: 0, Right: 1000, Bottom: 600},
			cursor:     point{X: 500, Y: 550},
			dpi:        240,
		},
		{
			name:       "top near MaxInt32",
			metrics:    hitTestMetrics{TitlebarHeight: maxInt32},
			windowRect: rect{Left: 0, Top: maxInt32 - 600, Right: 1000, Bottom: maxInt32},
			cursor:     point{X: 500, Y: maxInt32 - 1},
			dpi:        192,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nativeHitTestForRect(test.metrics, test.windowRect, test.cursor, test.dpi, false); got != htCaption {
				t.Fatalf("nativeHitTestForRect() = %d, want HTCAPTION", got)
			}
			if got := nativeCaptionButtonHitForRect(test.metrics, test.windowRect, test.cursor, test.dpi, false); got != htClient {
				t.Fatalf("nativeCaptionButtonHitForRect() = %d, want HTCLIENT outside controls", got)
			}
		})
	}
}

func TestNativeResolversKeepExtremeResizeEdgesDisjoint(t *testing.T) {
	const minInt32 = int32(-1 << 31)
	const maxInt32 = int32(1<<31 - 1)
	metrics := hitTestMetrics{ResizeBorder: maxInt32}
	tests := []struct {
		name       string
		windowRect rect
		edge       point
		midpoint   point
		want       int32
	}{
		{
			name:       "left near MaxInt32",
			windowRect: rect{Left: maxInt32 - 101, Top: 0, Right: maxInt32, Bottom: 101},
			edge:       point{X: maxInt32 - 101, Y: 50},
			midpoint:   point{X: maxInt32 - 51, Y: 50},
			want:       htLeft,
		},
		{
			name:       "right near MinInt32",
			windowRect: rect{Left: minInt32, Top: 0, Right: minInt32 + 101, Bottom: 101},
			edge:       point{X: minInt32 + 100, Y: 50},
			midpoint:   point{X: minInt32 + 50, Y: 50},
			want:       htRight,
		},
		{
			name:       "top near MaxInt32",
			windowRect: rect{Left: 0, Top: maxInt32 - 101, Right: 101, Bottom: maxInt32},
			edge:       point{X: 50, Y: maxInt32 - 101},
			midpoint:   point{X: 50, Y: maxInt32 - 51},
			want:       htTop,
		},
		{
			name:       "bottom near MinInt32",
			windowRect: rect{Left: 0, Top: minInt32, Right: 101, Bottom: minInt32 + 101},
			edge:       point{X: 50, Y: minInt32 + 100},
			midpoint:   point{X: 50, Y: minInt32 + 50},
			want:       htBottom,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nativeHitTestForRect(metrics, test.windowRect, test.edge, 192, false); got != test.want {
				t.Fatalf("nativeHitTestForRect(edge) = %d, want %d", got, test.want)
			}
			if got := nativeHitTestForRect(metrics, test.windowRect, test.midpoint, 192, false); got != htClient {
				t.Fatalf("nativeHitTestForRect(midpoint) = %d, want HTCLIENT between opposite bands", got)
			}
			if got := nativeCaptionButtonHitForRect(metrics, test.windowRect, test.edge, 192, false); got != htClient {
				t.Fatalf("nativeCaptionButtonHitForRect(edge) = %d, want HTCLIENT in resize band", got)
			}
		})
	}
}

func TestNativeResolversKeepExtremeScaledControlThirds(t *testing.T) {
	const minInt32 = int32(-1 << 31)
	windowRect := rect{Left: minInt32, Top: 0, Right: minInt32 + 600, Bottom: 600}
	metrics := hitTestMetrics{
		TitlebarHeight: 600,
		ControlsWidth:  1_500_000_000,
	}
	tests := []struct {
		name   string
		cursor point
		want   int32
	}{
		{name: "minimize", cursor: point{X: minInt32 + 100, Y: 100}, want: htMinButton},
		{name: "maximize", cursor: point{X: minInt32 + 300, Y: 100}, want: htMaxButton},
		{name: "close", cursor: point{X: minInt32 + 500, Y: 100}, want: htClose},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nativeCaptionButtonHitForRect(metrics, windowRect, test.cursor, 192, false); got != test.want {
				t.Fatalf("nativeCaptionButtonHitForRect() = %d, want %d", got, test.want)
			}
			if got := nativeHitTestForRect(metrics, windowRect, test.cursor, 192, false); got != htClient {
				t.Fatalf("nativeHitTestForRect() = %d, want HTCLIENT for production controls", got)
			}
		})
	}
}

func TestNativeHitTestForRectSkipsHugeResizeBandWhenMaximized(t *testing.T) {
	metrics := hitTestMetrics{
		ResizeBorder:   1_500_000_000,
		TitlebarHeight: 1_500_000_000,
	}
	windowRect := rect{Left: 0, Top: 0, Right: 1000, Bottom: 600}
	cursor := point{X: 500, Y: 300}
	if got := nativeHitTestForRect(metrics, windowRect, cursor, 192, false); got != htBottomRight {
		t.Fatalf("nativeHitTestForRect(restored) = %d, want HTBOTTOMRIGHT in bounded resize band", got)
	}
	if got := nativeHitTestForRect(metrics, windowRect, cursor, 192, true); got != htCaption {
		t.Fatalf("nativeHitTestForRect(maximized) = %d, want HTCAPTION after resize skip", got)
	}
}

func TestHitTestGeometryBoundsMetricBandsWithoutWrapping(t *testing.T) {
	windowRect := rect{Left: 0, Top: 0, Right: 10, Bottom: 7}
	for _, test := range []struct {
		name     string
		metric   int32
		resizeX  int64
		resizeY  int64
		title    int64
		controls int64
		wantHit  int32
	}{
		{name: "negative", metric: -1, wantHit: htClient},
		{name: "zero", metric: 0, wantHit: htClient},
		{name: "positive maximum", metric: int32(1<<31 - 1), resizeX: 5, resizeY: 3, title: 7, controls: 10, wantHit: htRight},
	} {
		t.Run(test.name, func(t *testing.T) {
			geometry, ok := newHitTestGeometry(hitTestMetrics{
				ResizeBorder:   test.metric,
				TitlebarHeight: test.metric,
				ControlsWidth:  test.metric,
			}, windowRect, point{X: 5, Y: 3}, ^uint32(0))
			if !ok {
				t.Fatal("newHitTestGeometry() ok = false")
			}
			if geometry.resizeWidth != test.resizeX || geometry.resizeHeight != test.resizeY {
				t.Fatalf("bounded resize = %d/%d, want %d/%d", geometry.resizeWidth, geometry.resizeHeight, test.resizeX, test.resizeY)
			}
			if got := geometry.titlebarBottom - geometry.top; got != test.title {
				t.Fatalf("bounded title height = %d, want %d", got, test.title)
			}
			if geometry.controlsWidth != test.controls {
				t.Fatalf("bounded controls width = %d, want %d", geometry.controlsWidth, test.controls)
			}
			if got := nativeHitTestForRect(hitTestMetrics{
				ResizeBorder:   test.metric,
				TitlebarHeight: test.metric,
				ControlsWidth:  test.metric,
			}, windowRect, point{X: 5, Y: 3}, ^uint32(0), false); got != test.wantHit {
				t.Fatalf("nativeHitTestForRect(%s metric) = %d, want %d", test.name, got, test.wantHit)
			}
		})
	}
}

func TestHitTestGeometrySupportsFullSignedRectSpan(t *testing.T) {
	const minInt32 = int32(-1 << 31)
	const maxInt32 = int32(1<<31 - 1)
	windowRect := rect{Left: minInt32, Top: minInt32, Right: maxInt32, Bottom: maxInt32}
	metrics := hitTestMetrics{
		ResizeBorder:   maxInt32,
		TitlebarHeight: maxInt32,
		ControlsWidth:  maxInt32,
	}
	geometry, ok := newHitTestGeometry(metrics, windowRect, point{}, ^uint32(0))
	if !ok {
		t.Fatal("newHitTestGeometry(full signed rect) ok = false")
	}
	const fullSpan = int64(1<<32 - 1)
	if geometry.right-geometry.left != fullSpan || geometry.bottom-geometry.top != fullSpan {
		t.Fatalf("rect extents = %d/%d, want %d/%d", geometry.right-geometry.left, geometry.bottom-geometry.top, fullSpan, fullSpan)
	}
	if geometry.resizeWidth != fullSpan/2 || geometry.resizeHeight != fullSpan/2 {
		t.Fatalf("resize bands = %d/%d, want independent midpoint %d", geometry.resizeWidth, geometry.resizeHeight, fullSpan/2)
	}
	if geometry.titlebarBottom != geometry.bottom || geometry.controlsLeft != geometry.left {
		t.Fatalf("bounded title/controls endpoints = %d/%d, want %d/%d", geometry.titlebarBottom, geometry.controlsLeft, geometry.bottom, geometry.left)
	}

	buttons := []struct {
		name   string
		cursor point
		want   int32
	}{
		{name: "minimize third", cursor: point{X: minInt32, Y: minInt32}, want: htMinButton},
		{name: "maximize third", cursor: point{X: -715827883, Y: minInt32}, want: htMaxButton},
		{name: "close third", cursor: point{X: 715827882, Y: minInt32}, want: htClose},
	}
	for _, test := range buttons {
		t.Run(test.name, func(t *testing.T) {
			if got := nativeCaptionButtonHitForRect(metrics, windowRect, test.cursor, ^uint32(0), true); got != test.want {
				t.Fatalf("nativeCaptionButtonHitForRect(full span, %s) = %d, want %d", test.name, got, test.want)
			}
		})
	}
}

func TestNativeCaptionButtonHitForRectSkipsResizeWhenMaximized(t *testing.T) {
	metrics := hitTestMetrics{ResizeBorder: 8, TitlebarHeight: 36, ControlsWidth: 18}
	windowRect := rect{Left: 0, Top: 0, Right: 100, Bottom: 100}
	cursor := point{X: 99, Y: 0}
	if got := nativeCaptionButtonHitForRect(metrics, windowRect, cursor, 96, false); got != htClient {
		t.Fatalf("nativeCaptionButtonHitForRect(resizable corner) = %d, want HTCLIENT", got)
	}
	if got := nativeCaptionButtonHitForRect(metrics, windowRect, cursor, 96, true); got != htClose {
		t.Fatalf("nativeCaptionButtonHitForRect(maximized corner) = %d, want HTCLOSE", got)
	}
}

func TestHitTestGeometryResizeBandsDoNotOverlapAtMidpoint(t *testing.T) {
	windowRect := rect{Left: 0, Top: 0, Right: 11, Bottom: 11}
	metrics := hitTestMetrics{ResizeBorder: int32(1<<31 - 1)}
	tests := []struct {
		x    int32
		want int32
	}{
		{x: 4, want: htLeft},
		{x: 5, want: htClient},
		{x: 6, want: htRight},
	}
	for _, test := range tests {
		geometry, ok := newHitTestGeometry(metrics, windowRect, point{X: test.x, Y: 5}, ^uint32(0))
		if !ok {
			t.Fatal("newHitTestGeometry() ok = false")
		}
		if geometry.resizeLeftEnd > geometry.resizeRightStart || geometry.resizeTopEnd > geometry.resizeBottomStart {
			t.Fatalf("opposite resize bands overlap: %#v", geometry)
		}
		if got := hitTestResizeBorder(geometry); got != test.want {
			t.Fatalf("hitTestResizeBorder(midpoint x=%d) = %d, want %d", test.x, got, test.want)
		}
	}
}

func TestNativeResolversRejectInvalidRectsAndOutsideCursors(t *testing.T) {
	validRect := rect{Left: 10, Top: 20, Right: 30, Bottom: 40}
	outside := []point{
		{X: 9, Y: 30},
		{X: 30, Y: 30},
		{X: 20, Y: 19},
		{X: 20, Y: 40},
	}
	for _, cursor := range outside {
		if _, ok := newHitTestGeometry(testMetrics, validRect, cursor, 96); ok {
			t.Fatalf("newHitTestGeometry(outside=%#v) ok = true", cursor)
		}
		if got := nativeHitTestForRect(testMetrics, validRect, cursor, 96, false); got != htClient {
			t.Fatalf("nativeHitTestForRect(outside=%#v) = %d, want HTCLIENT", cursor, got)
		}
		if got := nativeCaptionButtonHitForRect(testMetrics, validRect, cursor, 96, false); got != htClient {
			t.Fatalf("nativeCaptionButtonHitForRect(outside=%#v) = %d, want HTCLIENT", cursor, got)
		}
	}

	invalid := []rect{
		{Left: 10, Top: 20, Right: 10, Bottom: 40},
		{Left: 30, Top: 20, Right: 10, Bottom: 40},
		{Left: 10, Top: 40, Right: 30, Bottom: 40},
		{Left: 10, Top: 40, Right: 30, Bottom: 20},
	}
	for _, windowRect := range invalid {
		cursor := point{X: 10, Y: 20}
		if _, ok := newHitTestGeometry(testMetrics, windowRect, cursor, 96); ok {
			t.Fatalf("newHitTestGeometry(invalid=%#v) ok = true", windowRect)
		}
		if got := nativeHitTestForRect(testMetrics, windowRect, cursor, 96, true); got != htClient {
			t.Fatalf("nativeHitTestForRect(invalid=%#v) = %d, want HTCLIENT", windowRect, got)
		}
		if got := nativeCaptionButtonHitForRect(testMetrics, windowRect, cursor, 96, true); got != htClient {
			t.Fatalf("nativeCaptionButtonHitForRect(invalid=%#v) = %d, want HTCLIENT", windowRect, got)
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
	controlsWidth := int32(scaleLogicalPixels(testMetrics.ControlsWidth, 96))
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
	controlsWidth := int32(scaleLogicalPixels(testMetrics.ControlsWidth, 96))
	buttonWidth := controlsWidth / 3
	left := windowRect.Right - controlsWidth
	titlebarY := windowRect.Top + int32(scaleLogicalPixels(testMetrics.ResizeBorder, 96))
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
		border := int32(scaleLogicalPixels(testMetrics.ResizeBorder, dpi))
		titlebarHeight := int32(scaleLogicalPixels(testMetrics.TitlebarHeight, dpi))
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
		titlebarHeight := int32(scaleLogicalPixels(testMetrics.TitlebarHeight, dpi))
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
	got, ok := clampRectToArea(windowRect, workArea)
	if !ok {
		t.Fatal("clampRectToArea() ok = false")
	}
	if got.Top != workArea.Top {
		t.Fatalf("maximized top = %d, want %d", got.Top, workArea.Top)
	}
	if hit := nativeHitTestForRect(testMetrics, got, point{X: 500, Y: workArea.Top}, 120, true); hit != htCaption {
		t.Fatalf("maximized top band hit = %d, want HTCAPTION", hit)
	}

	extendedRect := rect{Left: -10, Top: -10, Right: 1930, Bottom: 1030}
	got, ok = clampRectToArea(extendedRect, workArea)
	if !ok {
		t.Fatal("clampRectToArea(extended) ok = false")
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

func TestResizeFallbackPointMapsFullSignedRectToNativeResizeHits(t *testing.T) {
	const minInt32 = int32(-1 << 31)
	const maxInt32 = int32(1<<31 - 1)
	windowRect := rect{Left: minInt32, Top: minInt32, Right: maxInt32, Bottom: maxInt32}
	tests := []struct {
		name string
		hit  int32
		want point
	}{
		{name: "left", hit: htLeft, want: point{X: minInt32, Y: -1}},
		{name: "right", hit: htRight, want: point{X: maxInt32 - 1, Y: -1}},
		{name: "top", hit: htTop, want: point{X: -1, Y: minInt32}},
		{name: "bottom", hit: htBottom, want: point{X: -1, Y: maxInt32 - 1}},
		{name: "top left", hit: htTopLeft, want: point{X: minInt32, Y: minInt32}},
		{name: "top right", hit: htTopRight, want: point{X: maxInt32 - 1, Y: minInt32}},
		{name: "bottom left", hit: htBottomLeft, want: point{X: minInt32, Y: maxInt32 - 1}},
		{name: "bottom right", hit: htBottomRight, want: point{X: maxInt32 - 1, Y: maxInt32 - 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cursor, ok := resizeFallbackPoint(windowRect, test.hit)
			if !ok {
				t.Fatalf("resizeFallbackPoint(%d) ok = false, want true", test.hit)
			}
			if cursor != test.want {
				t.Fatalf("resizeFallbackPoint(%d) = %#v, want full-span endpoint %#v", test.hit, cursor, test.want)
			}
			if got := nativeHitTestForRect(hitTestMetrics{ResizeBorder: 1}, windowRect, cursor, 96, false); got != test.hit {
				t.Fatalf("nativeHitTestForRect(fallback for %d) = %d, want %d", test.hit, got, test.hit)
			}
		})
	}
	if _, ok := resizeFallbackPoint(windowRect, htClient); ok {
		t.Fatal("resizeFallbackPoint(HTCLIENT) ok = true, want false")
	}
}

func TestResizeFallbackPointRejectsInvalidRects(t *testing.T) {
	tests := []struct {
		name       string
		windowRect rect
	}{
		{name: "zero width", windowRect: rect{Left: 10, Top: 20, Right: 10, Bottom: 40}},
		{name: "reversed width", windowRect: rect{Left: 30, Top: 20, Right: 10, Bottom: 40}},
		{name: "zero height", windowRect: rect{Left: 10, Top: 40, Right: 30, Bottom: 40}},
		{name: "reversed height", windowRect: rect{Left: 10, Top: 40, Right: 30, Bottom: 20}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if cursor, ok := resizeFallbackPoint(test.windowRect, htLeft); ok {
				t.Fatalf("resizeFallbackPoint(invalid rect) = %#v, true; want false", cursor)
			}
		})
	}
}
