//go:build windows

package host

import (
	"strconv"
	"strings"
	"sync"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

// nativeDiagnostics is the evidence the render watchdog reports when the
// frontend never signals readiness.
//
// "The window is up but the page is blank" has several distinct causes that look
// identical from the outside: the document never arrived, it arrived but its
// stylesheets 404'd, the scripts loaded but threw, or the page is fine and only
// the readiness call is missing. The counters below separate those cases without
// asking the user to reproduce anything.
type nativeDiagnostics struct {
	mu sync.Mutex

	lastFrontendPhase string
	lastAsset         assetDiagnostic
	documentCount     int
	stylesheetCount   int
	scriptCount       int
	lastBridge        string
}

type assetDiagnostic struct {
	name        string
	category    string
	method      string
	contentType string
	status      int
}

func newNativeDiagnostics() *nativeDiagnostics {
	return &nativeDiagnostics{lastFrontendPhase: "startup"}
}

func (diagnostics *nativeDiagnostics) recordAsset(response assetResponse, method string) {
	if diagnostics == nil {
		return
	}
	item := assetDiagnostic{
		name:        logsafe.FileName(response.request.path),
		category:    safeDiagnosticValue(response.request.category),
		method:      safeDiagnosticValue(method),
		contentType: safeDiagnosticValue(response.contentType),
		status:      response.status,
	}
	diagnostics.mu.Lock()
	// The favicon probe is unsolicited and always answered with 204; recording it
	// as "the last asset" would mask the request that actually mattered.
	if response.request.category != "favicon" {
		diagnostics.lastAsset = item
	}
	switch assetBucket(response.contentType) {
	case "document":
		diagnostics.documentCount++
	case "stylesheet":
		diagnostics.stylesheetCount++
	case "script":
		diagnostics.scriptCount++
	}
	diagnostics.mu.Unlock()
}

func (diagnostics *nativeDiagnostics) recordFrontendPhase(phase string) {
	if diagnostics == nil {
		return
	}
	diagnostics.mu.Lock()
	diagnostics.lastFrontendPhase = safeDiagnosticValue(phase)
	diagnostics.mu.Unlock()
}

func (diagnostics *nativeDiagnostics) recordBridge(method string, status string) {
	if diagnostics == nil {
		return
	}
	diagnostics.mu.Lock()
	diagnostics.lastBridge = safeDiagnosticValue(method) + ":" + safeDiagnosticValue(status)
	diagnostics.mu.Unlock()
}

func (diagnostics *nativeDiagnostics) timeoutSummary() string {
	if diagnostics == nil {
		return "phase=unknown, asset=unknown, asset_category=unknown, asset_status=0, document=0, stylesheet=0, script=0, last_bridge=unknown"
	}
	diagnostics.mu.Lock()
	defer diagnostics.mu.Unlock()
	phase := defaultDiagnosticValue(diagnostics.lastFrontendPhase)
	asset := diagnostics.lastAsset
	lastBridge := defaultDiagnosticValue(diagnostics.lastBridge)
	return "phase=" + phase +
		", asset=" + defaultDiagnosticValue(asset.name) +
		", asset_category=" + defaultDiagnosticValue(asset.category) +
		", asset_status=" + strconv.Itoa(asset.status) +
		", document=" + strconv.Itoa(diagnostics.documentCount) +
		", stylesheet=" + strconv.Itoa(diagnostics.stylesheetCount) +
		", script=" + strconv.Itoa(diagnostics.scriptCount) +
		", last_bridge=" + lastBridge
}

func assetBucket(contentType string) string {
	if strings.HasPrefix(contentType, "text/html") {
		return "document"
	}
	if strings.HasPrefix(contentType, "text/css") {
		return "stylesheet"
	}
	if strings.Contains(contentType, "javascript") {
		return "script"
	}
	return "other"
}

// logAssetResponseDebug logs the assets that carry diagnostic weight and stays
// quiet about the rest. A frontend can pull dozens of images and fonts on one
// load; logging each of them buries the three lines that tell you whether the
// document, its stylesheets and its scripts actually arrived.
func (provider *assetProvider) logAssetResponseDebug(response assetResponse, method string) {
	if response.status >= 400 || assetBucket(response.contentType) == "other" {
		return
	}
	provider.log.Debug("mullion: asset response served, status=" + strconv.Itoa(response.status) +
		", category=" + logsafe.Message(response.request.category) +
		", asset=" + logsafe.FileName(response.request.path) +
		", method=" + logsafe.Message(method) +
		", content_type=" + safeContentTypeForLog(response.contentType))
}

func safeDiagnosticValue(value string) string {
	return defaultDiagnosticValue(logsafe.Message(value))
}

func defaultDiagnosticValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func safeContentTypeForLog(contentType string) string {
	contentType = strings.TrimSpace(strings.ToLower(contentType))
	contentType = strings.ReplaceAll(contentType, "/", "_")
	contentType = strings.ReplaceAll(contentType, " ", "")
	contentType = strings.ReplaceAll(contentType, ";", "_")
	if contentType == "" {
		return "unknown"
	}
	return logsafe.Message(contentType)
}
