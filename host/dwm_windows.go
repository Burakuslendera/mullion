//go:build windows

package host

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	dwmapi = windows.NewLazySystemDLL("Dwmapi.dll")

	procDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
	procDwmDefWindowProc      = dwmapi.NewProc("DwmDefWindowProc")
	procDwmExtendFrame        = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
)

type dwmMargins struct {
	Left   int32
	Right  int32
	Top    int32
	Bottom int32
}

func setDWMWindowCornerPreference(hwnd windowHandle, preference int32) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	result, _, _ := procDwmSetWindowAttribute.Call(
		uintptr(hwnd),
		uintptr(dwmwaWindowCornerPreference),
		uintptr(unsafe.Pointer(&preference)),
		unsafe.Sizeof(preference),
	)
	if result != 0 {
		return hresultError(result)
	}
	return nil
}

func getDWMWindowCornerPreference(hwnd windowHandle) (int32, error) {
	var preference int32
	if err := getDWMWindowAttribute(hwnd, dwmwaWindowCornerPreference, unsafe.Pointer(&preference), unsafe.Sizeof(preference)); err != nil {
		return 0, err
	}
	return preference, nil
}

func setDWMWindowColorAttribute(hwnd windowHandle, attribute uintptr, color uint32) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	value := color
	result, _, _ := procDwmSetWindowAttribute.Call(
		uintptr(hwnd),
		attribute,
		uintptr(unsafe.Pointer(&value)),
		unsafe.Sizeof(value),
	)
	if result != 0 {
		return hresultError(result)
	}
	return nil
}

func getDWMExtendedFrameBounds(hwnd windowHandle) (rect, error) {
	var frame rect
	err := getDWMWindowAttribute(hwnd, dwmwaExtendedFrameBounds, unsafe.Pointer(&frame), unsafe.Sizeof(frame))
	return frame, err
}

func getDWMCaptionButtonBounds(hwnd windowHandle) (rect, error) {
	var bounds rect
	err := getDWMWindowAttribute(hwnd, dwmwaCaptionButtonBounds, unsafe.Pointer(&bounds), unsafe.Sizeof(bounds))
	return bounds, err
}

func getDWMWindowAttribute(hwnd windowHandle, attribute uintptr, value unsafe.Pointer, size uintptr) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	result, _, _ := procDwmGetWindowAttribute.Call(uintptr(hwnd), attribute, uintptr(value), size)
	if result != 0 {
		return hresultError(result)
	}
	return nil
}

func extendDWMFrameIntoClientArea(hwnd windowHandle, extend bool) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	margins := dwmMargins{}
	if extend {
		margins = dwmMargins{Left: 1, Right: 1, Top: 1, Bottom: 1}
	}
	result, _, _ := procDwmExtendFrame.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&margins)),
	)
	if result != 0 {
		return hresultError(result)
	}
	return nil
}

func dwmDefWindowProc(hwnd windowHandle, message uint32, wParam, lParam uintptr) (uintptr, bool) {
	var result uintptr
	handled, _, _ := procDwmDefWindowProc.Call(
		uintptr(hwnd),
		uintptr(message),
		wParam,
		lParam,
		uintptr(unsafe.Pointer(&result)),
	)
	return result, handled != 0
}

func hresultError(result uintptr) error {
	return fmt.Errorf("HRESULT 0x%08X", uint32(result))
}
