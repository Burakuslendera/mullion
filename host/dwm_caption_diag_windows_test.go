//go:build windows && mullion_dwm_caption_diag

package host

import "testing"

func TestDWMCaptionDiagnosticEnabledForBuildTag(t *testing.T) {
	if !nativeDWMCaptionDiagnosticEnabled() {
		t.Fatal("nativeDWMCaptionDiagnosticEnabled() = false, want true")
	}
}
