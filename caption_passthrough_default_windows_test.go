//go:build windows && !mullion_caption_passthrough_diag

package mullion

import "testing"

func TestCaptionPassthroughDiagnosticDisabledByDefault(t *testing.T) {
	if nativeCaptionPassthroughDiagnosticEnabled() {
		t.Fatal("nativeCaptionPassthroughDiagnosticEnabled() = true, want false")
	}
	if shouldUseCaptionPassthroughForPolicy(htMaxButton, nativeDWMCaptionPolicyMaximizeOnly) {
		t.Fatal("caption passthrough must stay disabled in production default")
	}
}
