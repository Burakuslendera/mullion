//go:build windows && mullion_caption_passthrough_diag

package mullion

import "testing"

func TestCaptionPassthroughDiagnosticOnlyAllowsMaximizeCandidate(t *testing.T) {
	if !nativeCaptionPassthroughDiagnosticEnabled() {
		t.Fatal("nativeCaptionPassthroughDiagnosticEnabled() = false, want true")
	}
	if !shouldUseCaptionPassthroughForPolicy(htMaxButton, nativeDWMCaptionPolicyMaximizeOnly) {
		t.Fatal("caption passthrough diagnostic rejected HTMAXBUTTON")
	}
	if shouldUseCaptionPassthroughForPolicy(htClose, nativeDWMCaptionPolicyMaximizeOnly) {
		t.Fatal("caption passthrough diagnostic accepted HTCLOSE")
	}
	if shouldUseCaptionPassthroughForPolicy(htMinButton, nativeDWMCaptionPolicyMaximizeOnly) {
		t.Fatal("caption passthrough diagnostic accepted HTMINBUTTON")
	}
	if shouldUseCaptionPassthroughForPolicy(htMaxButton, nativeDWMCaptionPolicyAllButtons) {
		t.Fatal("caption passthrough diagnostic must not widen all-buttons DWM routing")
	}
}
