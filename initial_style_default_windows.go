//go:build windows

package mullion

func nativeInitialWindowStyle() uintptr {
	return uintptr(wsNativeWindow)
}

func nativeInitialWindowStyleName() string {
	return "native"
}
