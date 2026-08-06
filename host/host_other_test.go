//go:build !windows

package host

import (
	"errors"
	"testing"
)

func TestNonWindowsRunRemainsAPlatformError(t *testing.T) {
	err := New(Config{}).Run()
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("Run error = %v, want ErrUnsupportedPlatform", err)
	}
	if errors.Is(err, ErrUnsupportedArchitecture) {
		t.Fatalf("Run error = %v, must not report a Windows architecture error", err)
	}
}
