//go:build windows && mullion_dwm_caption_diag

package mullion

func nativeDWMCaptionDiagnosticEnabled() bool {
	return true
}
