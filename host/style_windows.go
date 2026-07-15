//go:build windows

package host

import (
	"strconv"

	"golang.org/x/sys/windows"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

const nativeFrameChangedFlags = swpNoMove | swpNoSize | swpNoZOrder | swpNoActivate | swpFrameChanged

func (host *Host) applyNativeWindowStyle(hwnd windowHandle) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	style, err := windowStyle(hwnd)
	if err != nil {
		return err
	}
	host.logNativeWindowStyle(hwnd, "before")
	profile := activeNativeFrameProfile()
	host.log.Debug("mullion: native frame profile selected, profile=" + string(profile))
	next := styleForNativeFrameProfile(profile, style)
	if err := setWindowStyle(hwnd, next); err != nil {
		return err
	}
	host.logNativeWindowStyle(hwnd, "after_set")
	if err := host.setWindowFrameChanged(hwnd); err != nil {
		return err
	}
	applied, err := windowStyle(hwnd)
	if err != nil {
		return err
	}
	if !nativeFrameProfileMatchesStyle(profile, applied) {
		host.log.Warn("mullion: native frame profile mismatch, profile=" + string(profile))
	}
	host.applyDWMFramePreferences(hwnd)
	return nil
}

func windowStyle(hwnd windowHandle) (uintptr, error) {
	if hwnd == 0 {
		return 0, windows.ERROR_INVALID_WINDOW_HANDLE
	}
	result, _, err := procGetWindowLongPtr.Call(uintptr(hwnd), windowLongIndex(gwlStyle))
	if result == 0 && err != windows.ERROR_SUCCESS {
		return 0, syscallError(err)
	}
	return result, nil
}

func windowExStyle(hwnd windowHandle) (uintptr, error) {
	if hwnd == 0 {
		return 0, windows.ERROR_INVALID_WINDOW_HANDLE
	}
	result, _, err := procGetWindowLongPtr.Call(uintptr(hwnd), windowLongIndex(gwlExStyle))
	if result == 0 && err != windows.ERROR_SUCCESS {
		return 0, syscallError(err)
	}
	return result, nil
}

func setWindowStyle(hwnd windowHandle, style uintptr) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	result, _, err := procSetWindowLongPtr.Call(uintptr(hwnd), windowLongIndex(gwlStyle), style)
	if result == 0 && err != windows.ERROR_SUCCESS {
		return syscallError(err)
	}
	return nil
}

func setWindowExStyle(hwnd windowHandle, exStyle uintptr) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	result, _, err := procSetWindowLongPtr.Call(uintptr(hwnd), windowLongIndex(gwlExStyle), exStyle)
	if result == 0 && err != windows.ERROR_SUCCESS {
		return syscallError(err)
	}
	return nil
}

func windowLongIndex(index int32) uintptr {
	return uintptr(int(index))
}

// setWindowFrameChanged asks the shell to recompute the non-client frame after
// a style change.
//
// SWP_NOMOVE|SWP_NOSIZE are not optional here. SWP_FRAMECHANGED is delivered via
// SetWindowPos, and SetWindowPos reads the position and size arguments unless it
// is told to ignore them. Passing zeros without those two flags does not mean
// "leave it alone" - it means "move to 0,0 and resize to 0x0", which collapses
// the client area to a few dozen pixels and renders the WebView into a sliver.
func (host *Host) setWindowFrameChanged(hwnd windowHandle) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	result, _, err := procSetWindowPos.Call(
		uintptr(hwnd),
		0,
		0,
		0,
		0,
		0,
		nativeFrameChangedFlags,
	)
	if result == 0 {
		return syscallError(err)
	}
	host.logNativeWindowStyle(hwnd, "after_framechanged")
	return nil
}

func (host *Host) applyDWMFramePreferences(hwnd windowHandle) {
	if err := extendDWMFrameIntoClientArea(hwnd, true); err != nil {
		host.log.Warn("mullion: dwm frame extension failed, reason=" + logsafe.Reason(err))
		host.logNativeWindowStyle(hwnd, "after_dwm_frame_failed")
	} else {
		host.log.Debug("mullion: dwm frame extension applied, margins=1")
		host.logNativeWindowStyle(hwnd, "after_dwm_frame")
	}
	if err := setDWMWindowCornerPreference(hwnd, dwmWindowCornerPreferenceRnd); err != nil {
		host.log.Warn("mullion: dwm corner preference failed, reason=" + logsafe.Reason(err))
		host.logNativeWindowStyle(hwnd, "after_dwm_corner_failed")
		return
	}
	preference, err := getDWMWindowCornerPreference(hwnd)
	if err != nil {
		host.log.Warn("mullion: dwm corner preference readback failed, reason=" + logsafe.Reason(err))
		host.logNativeWindowStyle(hwnd, "after_dwm_corner_unreadable")
		return
	}
	host.log.Debug("mullion: dwm corner preference applied, requested=round, readback=" + strconv.FormatInt(int64(preference), 10))
	host.logNativeWindowStyle(hwnd, "after_dwm_corner")
}
