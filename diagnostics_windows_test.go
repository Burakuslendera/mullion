//go:build windows

package mullion

import (
	"strings"
	"testing"
)

func TestNativeDiagnosticsTimeoutSummary(t *testing.T) {
	diagnostics := newNativeDiagnostics()
	diagnostics.recordFrontendPhase("frontend boot started")
	diagnostics.recordAsset(assetResponse{
		status:      200,
		contentType: "text/html; charset=utf-8",
		request:     assetRequest{path: "index.html", category: "asset"},
	}, "GET")
	diagnostics.recordAsset(assetResponse{
		status:      200,
		contentType: "text/css; charset=utf-8",
		request:     assetRequest{path: "style.css", category: "asset"},
	}, "GET")
	diagnostics.recordAsset(assetResponse{
		status:      200,
		contentType: "text/javascript; charset=utf-8",
		request:     assetRequest{path: "app.js", category: "asset"},
	}, "GET")
	diagnostics.recordBridge("Ping", "completed")

	summary := diagnostics.timeoutSummary()
	for _, expected := range []string{
		"phase=frontend boot started",
		"asset=app.js",
		"asset_category=asset",
		"asset_status=200",
		"document=1",
		"stylesheet=1",
		"script=1",
		"last_bridge=Ping:completed",
	} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("summary missing %q:\n%s", expected, summary)
		}
	}
}

// TestNativeDiagnosticsIgnoresFaviconAsLastAsset locks a small but load-bearing
// detail: the browser probes /favicon.ico unprompted, and if that probe were
// recorded as "the last asset" it would mask the request that actually failed.
func TestNativeDiagnosticsIgnoresFaviconAsLastAsset(t *testing.T) {
	diagnostics := newNativeDiagnostics()
	diagnostics.recordAsset(assetResponse{
		status:      404,
		contentType: "text/plain",
		request:     assetRequest{path: "app.js", category: "missing"},
	}, "GET")
	diagnostics.recordAsset(assetResponse{
		status:      204,
		contentType: "image/x-icon",
		request:     assetRequest{path: "favicon.ico", category: "favicon"},
	}, "GET")

	summary := diagnostics.timeoutSummary()
	if !strings.Contains(summary, "asset=app.js") || !strings.Contains(summary, "asset_status=404") {
		t.Fatalf("favicon probe overwrote the last real asset:\n%s", summary)
	}
}

// TestFrontendDiagnosticAssetSanitizesPath locks the promise made to Logger
// implementations: a message handed to them never carries a user's file path.
func TestFrontendDiagnosticAssetSanitizesPath(t *testing.T) {
	got := frontendDiagnosticAsset(`C:\Users\Example User\AppData\Acme\src\secret.js`)
	if got != "secret.js" {
		t.Fatalf("frontendDiagnosticAsset() = %q, want secret.js", got)
	}
}
