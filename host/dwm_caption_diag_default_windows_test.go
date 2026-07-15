//go:build windows && !mullion_dwm_caption_diag

package host

import "testing"

func TestDWMCaptionDiagnosticDisabledByDefault(t *testing.T) {
	if nativeDWMCaptionDiagnosticEnabled() {
		t.Fatal("nativeDWMCaptionDiagnosticEnabled() = true, want false")
	}
}
