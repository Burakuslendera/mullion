//go:build windows && mullion_script_completion_delay_diag

package main

import "testing"

func TestForbiddenPostCancellationSuccessMarkersIncludeFrontendReady(t *testing.T) {
	want := "frontend ready"
	for _, marker := range forbiddenPostCancellationSuccessMarkers() {
		if marker == want {
			return
		}
	}
	t.Fatalf("forbidden post-cancellation success markers omit %q", want)
}
