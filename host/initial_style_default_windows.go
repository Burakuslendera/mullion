//go:build windows

package host

func nativeInitialWindowStyle() uintptr {
	return uintptr(wsNativeWindow)
}

func nativeInitialWindowStyleName() string {
	return "native"
}
