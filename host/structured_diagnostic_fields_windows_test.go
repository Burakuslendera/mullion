//go:build windows

package host

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// TestStructuredDiagnosticEmittersKeepSingleFieldValues sends delimiter-bearing
// frontend and asset inputs through every structured diagnostic producer. The
// logger's comma-separated key=value grammar must retain one authoritative value
// per key; free-text fields use a separate reducer and are deliberately absent.
func TestStructuredDiagnosticEmittersKeepSingleFieldValues(t *testing.T) {
	const forged = ", forged=1"

	t.Run("trusted frontend resource diagnostic", func(t *testing.T) {
		host, logger := newTestHost(t, Config{})
		host.recordFrontendDiagnostic("resource-script"+forged, "https://mullion.localhost/a"+forged+".js")
		assertStructuredFields(t, logMessage(t, logger.String()), "kind", "asset")
	})

	t.Run("asset error", func(t *testing.T) {
		logger := &captureLogger{}
		provider := newAssetProvider(nil, newLogSink(logger), canonicalOrigin{}, nil)
		provider.logAssetResponseError(assetResponse{
			status:  http.StatusNotFound,
			reason:  "Not Found" + forged,
			request: assetRequest{category: "missing" + forged, path: "a" + forged + ".js"},
		})
		assertStructuredFields(t, logMessage(t, logger.String()), "status", "category", "asset")
	})

	t.Run("asset served", func(t *testing.T) {
		logger := &captureLogger{}
		provider := newAssetProvider(nil, newLogSink(logger), canonicalOrigin{}, nil)
		provider.logAssetResponseDebug(assetResponse{
			status:      200,
			contentType: "text/javascript" + forged,
			request:     assetRequest{category: "asset" + forged, path: "a" + forged + ".js"},
		}, "GET"+forged)
		assertStructuredFields(t, logMessage(t, logger.String()), "status", "category", "asset", "method", "content_type")
	})

	t.Run("application bridge", func(t *testing.T) {
		method := "Call" + forged
		var received string
		host, logger := newTestHost(t, Config{Bridge: func(raw string) string {
			received = raw
			return ""
		}})
		raw := `{"id":"1","method":` + strconv.Quote(method) + `,"args":[]}`
		host.handleWebMessage(raw, true)
		if received != raw {
			t.Fatal("structured logging rewrote Config.Bridge's raw request")
		}
		for _, line := range strings.Split(strings.TrimSpace(logger.String()), "\n") {
			if strings.Contains(line, "mullion: bridge method ") {
				assertStructuredFields(t, logMessage(t, line), "method")
			}
		}
	})

	t.Run("retained watchdog summary", func(t *testing.T) {
		diagnostics := newNativeDiagnostics()
		diagnostics.recordFrontendPhase("ready" + forged)
		diagnostics.recordAsset(assetResponse{
			status:      200,
			contentType: "text/javascript" + forged,
			request:     assetRequest{category: "asset" + forged, path: "a" + forged + ".js"},
		}, "GET"+forged)
		diagnostics.recordBridge("Call"+forged, "completed"+forged)
		assertStructuredFields(t, diagnostics.timeoutSummary(), "phase", "asset", "asset_category", "asset_status", "document", "stylesheet", "script", "last_bridge")
	})
}

func logMessage(t *testing.T, line string) string {
	t.Helper()
	message, ok := strings.CutPrefix(line, "level=")
	if !ok {
		t.Fatalf("log line has no level prefix: %q", line)
	}
	_, message, ok = strings.Cut(message, " msg=")
	if !ok {
		t.Fatalf("log line has no message: %q", line)
	}
	return message
}

func assertStructuredFields(t *testing.T, message string, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if strings.Count(message, key+"=") != 1 {
			t.Fatalf("structured key %q count = %d, want 1 in %q", key, strings.Count(message, key+"="), message)
		}
	}
	start := strings.Index(message, keys[0]+"=")
	if start < 0 {
		t.Fatalf("structured key %q is absent from %q", keys[0], message)
	}
	fields := strings.Split(message[start:], ", ")
	if len(fields) != len(keys) {
		t.Fatalf("structured field count = %d, want %d in %q", len(fields), len(keys), message)
	}
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" || value == "" {
			t.Fatalf("invalid structured field %q in %q", field, message)
		}
	}
}
