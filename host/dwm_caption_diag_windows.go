//go:build windows && mullion_dwm_caption_diag

package host

func nativeDWMCaptionDiagnosticEnabled() bool {
	return true
}
