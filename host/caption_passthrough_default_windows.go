//go:build windows && !mullion_caption_passthrough_diag

package host

func nativeCaptionPassthroughDiagnosticEnabled() bool {
	return false
}
