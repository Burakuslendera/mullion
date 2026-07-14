//go:build windows && mullion_dwm_caption_diag

package mullion

import "testing"

func TestDWMCaptionDiagnosticEnabledForBuildTag(t *testing.T) {
	if !nativeDWMCaptionDiagnosticEnabled() {
		t.Fatal("nativeDWMCaptionDiagnosticEnabled() = false, want true")
	}
}
