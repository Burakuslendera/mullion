//go:build windows

package mullion

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	tmeHover     = 0x00000001
	tmeLeave     = 0x00000002
	tmeNonClient = 0x00000010
	hoverDefault = 0xFFFFFFFF
)

var procTrackMouseEvent = user32.NewProc("TrackMouseEvent")

type trackMouseEvent struct {
	Size      uint32
	Flags     uint32
	HwndTrack windowHandle
	HoverTime uint32
}

func (host *Host) trackNativeTooltipMouse(hwnd windowHandle) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	event := trackMouseEvent{
		Size:      uint32(unsafe.Sizeof(trackMouseEvent{})),
		Flags:     tmeHover | tmeLeave | tmeNonClient,
		HwndTrack: hwnd,
		HoverTime: hoverDefault,
	}
	result, _, err := procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&event)))
	if result == 0 {
		return syscallError(err)
	}
	return nil
}
