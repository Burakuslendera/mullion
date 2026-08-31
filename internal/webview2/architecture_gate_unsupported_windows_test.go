//go:build windows && !amd64

package webview2

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

// These are the real production entry points exercised by the Windows/386 CI
// job under WOW64. None may reach discovery, DLL loading, or the shared COM
// callback factory on an unsupported process ABI.
func TestUnsupportedArchitectureProductionEntriesPrecedeCOMCallbacks(t *testing.T) {
	originalFactory := newCOMCallback
	allocations := 0
	newCOMCallback = func(callback any) uintptr {
		allocations++
		return originalFactory(callback)
	}
	t.Cleanup(func() { newCOMCallback = originalFactory })

	assertUnsupported := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrUnsupportedArchitecture) {
			t.Fatalf("%s error = %v, want ErrUnsupportedArchitecture", name, err)
		}
		for _, want := range []string{"GOARCH=" + runtime.GOARCH, "windows/amd64"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s error = %q, want %q", name, err, want)
			}
		}
	}

	assertUnsupported("ValidateArchitecture", ValidateArchitecture())
	_, _, err := FindRuntime()
	assertUnsupported("FindRuntime", err)
	_, err = RuntimeClientPath()
	assertUnsupported("RuntimeClientPath", err)
	report, err := DescribeRuntime()
	assertUnsupported("DescribeRuntime", err)
	if report.ExportName == "" {
		t.Fatal("DescribeRuntime did not name the export it refused to inspect")
	}
	handler := NewWebMessageReceivedHandler(func(*ICoreWebView2, *ICoreWebView2WebMessageReceivedEventArgs) {})
	if handler != nil {
		ReleaseHandler(handler)
		t.Fatal("unsupported callback constructor returned a COM object")
	}
	_, err = CreateEnvironmentWithOptions(Options{})
	assertUnsupported("CreateEnvironmentWithOptions", err)

	if allocations != 0 {
		t.Fatalf("unsupported production entries allocated %d COM callbacks, want 0", allocations)
	}
	if iunknownVtbl != (IUnknownVtbl{}) || eventHandlerVtable != (eventHandlerVtbl{}) ||
		completedVtable != (completionVtbl{}) || scriptCompletionVtable != (scriptCompletionVtbl{}) ||
		environmentOptionsVtable != (environmentOptionsVtbl{}) {
		t.Fatal("unsupported production entry initialized a COM vtable")
	}
}
