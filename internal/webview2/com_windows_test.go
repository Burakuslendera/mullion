//go:build windows

package webview2

// The HRESULT boundary: hres decides what counts as a failure, and HResultError
// carries the raw code so a caller can tell an old runtime (E_NOINTERFACE from a
// QueryInterface for an interface it does not have) from a broken one. The codes
// are supplied by hand, so nothing here touches COM or the runtime.

import (
	"errors"
	"strings"
	"testing"
)

func TestHResult(t *testing.T) {
	if err := hres(0); err != nil {
		t.Errorf("S_OK must be a success, got %v", err)
	}
	// S_FALSE is a success. Reading it as a failure is a classic COM bug: some
	// methods use it to mean "nothing to do".
	if err := hres(1); err != nil {
		t.Errorf("S_FALSE must be a success, got %v", err)
	}
	err := hres(eNoInterface)
	if err == nil {
		t.Fatal("E_NOINTERFACE must be an error")
	}
	var code HResultError
	if !errors.As(err, &code) {
		t.Fatalf("error %v does not carry its HRESULT; callers cannot tell an old runtime from a broken one", err)
	}
	if code.HResult() != 0x80004002 {
		t.Errorf("HResult() = 0x%08X, want 0x80004002", code.HResult())
	}
	if !strings.Contains(err.Error(), "80004002") {
		t.Errorf("error %q does not name the code", err)
	}
}
