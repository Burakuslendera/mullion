//go:build windows

package host

// activeNativeFrameProfile is the frame profile the host runs with.
//
// CaptionSysMenuNCCalc keeps WS_CAPTION, so the shell still treats the window as a
// normal top-level window (Snap, Alt+Tab and the minimise/restore animations all
// key off it), and adds WS_SYSMENU, which is what makes a right-click on the
// app-region caption open the standard system menu. WM_NCCALCSIZE then extends the
// client area over the frame, so the WebView covers the whole window and the title
// bar is HTML.
//
// DWM maximize-button forwarding and the synthetic HTMAXBUTTON hit-test are
// deliberately left out. They exist to make Windows show the Snap-layout flyout on
// maximize-button hover, but they hand caption message handling back to DWM on a
// window that has no native caption, which destabilises the frame. Snap stays
// reachable through Win+Z and by dragging to a screen edge.
func activeNativeFrameProfile() nativeFrameProfile {
	return nativeFrameProfileCaptionSysMenuNCCalc
}
