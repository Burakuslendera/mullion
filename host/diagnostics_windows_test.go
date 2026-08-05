//go:build windows

package host

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/Burakuslendera/mullion/internal/logsafe"
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

func TestNativeDiagnosticsBoundsAndDetachesFrontendControlledState(t *testing.T) {
	diagnostics := newNativeDiagnostics()
	path := strings.Repeat("x", logsafe.DiagnosticLimit*4) + `/tiny.js`
	large := strings.Repeat(".:;,", logsafe.DiagnosticLimit*2)
	diagnostics.recordAsset(assetResponse{
		status:      200,
		contentType: "text/javascript",
		request:     assetRequest{path: path, category: large},
	}, large)

	diagnostics.mu.Lock()
	asset := diagnostics.lastAsset
	diagnostics.mu.Unlock()
	if asset.name != "tiny.js" {
		t.Fatalf("retained asset name = %q, want tiny.js", asset.name)
	}
	for name, value := range map[string]string{
		"name": asset.name, "category": asset.category, "method": asset.method,
	} {
		if len(value) > logsafe.DiagnosticLimit {
			t.Fatalf("retained asset %s len = %d, limit = %d", name, len(value), logsafe.DiagnosticLimit)
		}
	}
	pathStart := uintptr(unsafe.Pointer(unsafe.StringData(path)))
	nameStart := uintptr(unsafe.Pointer(unsafe.StringData(asset.name)))
	if nameStart >= pathStart && nameStart < pathStart+uintptr(len(path)) {
		t.Fatal("retained asset name shares the large request path's backing storage")
	}
}
