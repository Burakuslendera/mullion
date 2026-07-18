//go:build windows

package webview2

import (
	"runtime"
	"testing"
	"unsafe"
)

// TestSettingsReleaseDropsExactlyOneReference locks the Release shim for the
// base settings object. GetSettings returns an owned reference
// (interfaces_windows.go), and until this shim existed nothing outside the
// package could drop it - so every Embed pinned the two settings references the
// host takes (webview hardening and tab-strip startup) for the life of the
// process. The fake vtable is the real ICoreWebView2SettingsVtbl, so the call
// also proves the shim lands on the IUnknown Release slot rather than on a
// settings method.
func TestSettingsReleaseDropsExactlyOneReference(t *testing.T) {
	vtbl := ICoreWebView2SettingsVtbl{IUnknownVtbl: fakeComIUnknownVtbl}
	settings := &ICoreWebView2Settings{Vtbl: &vtbl}
	state := &fakeComState{}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(settings)), state))

	settings.Release()

	if got := state.releases; got != 1 {
		t.Fatalf("releases = %d, want exactly 1: fewer leaks the settings object, more frees an object the runtime still owns", got)
	}
	if got := state.addRefs; got != 0 {
		t.Fatalf("addRefs = %d, want 0: Release must not land on another IUnknown slot", got)
	}
	runtime.KeepAlive(settings)
}
