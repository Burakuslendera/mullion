//go:build windows

package host

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPerMonitorV2DPIAwarenessContext(t *testing.T) {
	if dpiAwarenessContextPerMonitorAwareV2 != ^uintptr(3) {
		t.Fatalf("dpi context = %d, want per-monitor-v2 pseudo handle", dpiAwarenessContextPerMonitorAwareV2)
	}
}

func TestClassifyDPIAwarenessResultPreservesNativeErrorIdentity(t *testing.T) {
	nativeErr := errors.New("native DPI refusal")
	tests := []struct {
		name         string
		result       uintptr
		alreadyAware bool
		callErr      error
		wantSuccess  bool
		wantErr      error
	}{
		{name: "native success", result: 1, callErr: nativeErr, wantSuccess: true},
		{name: "already aware refusal", alreadyAware: true, callErr: nativeErr, wantSuccess: true},
		{name: "native failure", callErr: nativeErr, wantErr: nativeErr},
		{name: "unknown failure leaves caller fallback", callErr: windows.ERROR_SUCCESS, wantErr: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			success, err := classifyDPIAwarenessResult(test.result, test.alreadyAware, test.callErr)
			if success != test.wantSuccess {
				t.Fatalf("classifyDPIAwarenessResult success = %v, want %v", success, test.wantSuccess)
			}
			if err != test.wantErr {
				t.Fatalf("classifyDPIAwarenessResult error = %v, want exact %v", err, test.wantErr)
			}
		})
	}
}
