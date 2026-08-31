//go:build windows && !mullion_script_completion_delay_diag

package webview2

// The ordinary build selects this no-op implementation and publishes a genuine
// required-script callback inline. The diagnostic-tag twin may delay only that
// publication for bounded live proof.
func delayRequiredScriptCompletionPublication(func() bool) bool {
	return false
}
