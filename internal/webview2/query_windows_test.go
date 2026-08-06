//go:build windows

package webview2

import (
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

// TestSettingsReleaseDropsExactlyOneReference locks the Release shim for the
// base settings object. GetSettings returns an owned reference
// (interfaces_core_windows.go), and until this shim existed nothing outside the
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
func TestSHCreateMemStreamSizeRejectsValuesOutsideUINT(t *testing.T) {
	tests := []struct {
		name    string
		length  uint64
		want    uint32
		wantErr bool
	}{
		{name: "empty", length: 0, want: 0},
		{name: "maximum UINT", length: maxSHCreateMemStreamSize, want: ^uint32(0)},
		{name: "one past UINT", length: maxSHCreateMemStreamSize + 1, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := shCreateMemStreamSize(test.length)
			if (err != nil) != test.wantErr {
				t.Fatalf("shCreateMemStreamSize(%d) error = %v, wantErr %v", test.length, err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("shCreateMemStreamSize(%d) = %d, want %d", test.length, got, test.want)
			}
		})
	}
}

// LazyProc.Call's uintptrescapes contract only applies when the Go pointer is
// converted in its argument list. Escape diagnostics lock that compiler-visible
// lifetime without calling SHCreateMemStream from the headless test suite.
func TestNewMemoryStreamPinsContentAtSyscallBoundary(t *testing.T) {
	output, err := exec.Command("go", "build", "-gcflags=-m=2", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("compile WebView2 escape diagnostics: %v\n%s", err, output)
	}
	diagnostics := string(output)
	escapeMarker := regexp.MustCompile(`(?m)^.*query_windows\.go:\d+:\d+: parameter content leaks to \{heap\}(?: for NewMemoryStream)? with derefs=0:$`)
	location := escapeMarker.FindStringIndex(diagnostics)
	start := -1
	if location != nil {
		start = location[0]
	}
	if start < 0 {
		t.Fatal("NewMemoryStream content no longer escapes at the syscall boundary")
	}
	end := strings.Index(diagnostics[start:], "leaking param: content")
	if end < 0 || !strings.Contains(diagnostics[start:start+end], "//go:uintptrescapes") {
		t.Fatal("NewMemoryStream content escape is no longer caused by uintptrescapes")
	}
}
