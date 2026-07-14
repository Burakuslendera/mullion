//go:build windows && mullion_caption_passthrough_diag

package mullion

func nativeCaptionPassthroughDiagnosticEnabled() bool {
	return true
}
