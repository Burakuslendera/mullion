//go:build windows

package host

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

const testVirtualHost = defaultVirtualHost

var testOrigin = "https://" + testVirtualHost

func newTestAssetProvider(assets fs.FS) assetProvider {
	return newAssetProvider(assets, newLogSink(NopLogger{}), testVirtualHost, newNativeDiagnostics())
}

func TestResolveAssetPath(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    string
		wantErr int
	}{
		{name: "root", uri: testOrigin + "/", want: "index.html"},
		{name: "index", uri: testOrigin + "/index.html", want: "index.html"},
		{name: "nested", uri: testOrigin + "/assets/app.js", want: "assets/app.js"},
		{name: "query stripped", uri: testOrigin + "/assets/app.js?v=1", want: "assets/app.js"},
		{name: "wrong host", uri: "https://example.test/index.html", wantErr: http.StatusForbidden},
		{name: "wrong scheme", uri: "http://" + testVirtualHost + "/index.html", wantErr: http.StatusForbidden},
		{name: "traversal", uri: testOrigin + "/../secret", wantErr: http.StatusForbidden},
		{name: "encoded traversal", uri: testOrigin + "/%2e%2e/secret", wantErr: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := resolveAssetPath(testVirtualHost, test.uri)
			if got != test.want || gotErr != test.wantErr {
				t.Fatalf("resolveAssetPath() = %q, %d, want %q, %d", got, gotErr, test.want, test.wantErr)
			}
		})
	}
}

// TestResolveAssetRequestHostIsConfigured locks the fix for a latent bug: the
// origin the WebView navigates to and the host this allow-list accepts used to
// be two independent literals. They now both come from Config.VirtualHost, so a
// custom host must be accepted and the default must not.
func TestResolveAssetRequestHostIsConfigured(t *testing.T) {
	const custom = "example.internal"
	got, status := resolveAssetRequest(custom, "https://"+custom+"/index.html")
	if status != 0 || got.path != "index.html" {
		t.Fatalf("custom virtual host rejected: %q, %d", got.path, status)
	}
	if _, status := resolveAssetRequest(custom, testOrigin+"/index.html"); status != http.StatusForbidden {
		t.Fatalf("default host accepted under a custom virtual host: %d", status)
	}
}

func TestAssetProviderResolve(t *testing.T) {
	provider := newTestAssetProvider(fstest.MapFS{
		"index.html":     &fstest.MapFile{Data: []byte("<html></html>")},
		"style.css":      &fstest.MapFile{Data: []byte("body{}")},
		"app.js":         &fstest.MapFile{Data: []byte("window.x={}")},
		"data/app.json":  &fstest.MapFile{Data: []byte("{}")},
		"image/icon.svg": &fstest.MapFile{Data: []byte("<svg></svg>")},
		"image/icon.png": &fstest.MapFile{Data: []byte{0x89, 0x50, 0x4e, 0x47}},
		"empty":          &fstest.MapFile{Data: nil},
	})

	tests := []struct {
		name        string
		uri         string
		wantStatus  int
		wantContent string
	}{
		{name: "html", uri: testOrigin + "/", wantStatus: http.StatusOK, wantContent: "text/html"},
		{name: "css", uri: testOrigin + "/style.css", wantStatus: http.StatusOK, wantContent: "text/css"},
		{name: "js", uri: testOrigin + "/app.js", wantStatus: http.StatusOK, wantContent: "text/javascript"},
		{name: "json", uri: testOrigin + "/data/app.json", wantStatus: http.StatusOK, wantContent: "application/json"},
		{name: "svg", uri: testOrigin + "/image/icon.svg", wantStatus: http.StatusOK, wantContent: "image/svg+xml"},
		{name: "png", uri: testOrigin + "/image/icon.png", wantStatus: http.StatusOK, wantContent: "image/png"},
		{name: "favicon", uri: testOrigin + "/favicon.ico", wantStatus: http.StatusNoContent, wantContent: "image/x-icon"},
		{name: "missing", uri: testOrigin + "/missing.js", wantStatus: http.StatusNotFound, wantContent: "text/plain"},
		{name: "traversal", uri: testOrigin + "/../secret", wantStatus: http.StatusForbidden, wantContent: "text/plain"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := provider.resolve(test.uri)
			if response.status != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.status, test.wantStatus)
			}
			if !containsHeader(response.headers, "Content-Type: "+test.wantContent) {
				t.Fatalf("headers = %q, want content type %q", response.headers, test.wantContent)
			}
			if !containsHeader(response.headers, "Cache-Control: no-store") {
				t.Fatalf("headers = %q, want no-store cache control", response.headers)
			}
		})
	}
}

func TestResolveAssetRequestDiagnostic(t *testing.T) {
	tests := []struct {
		name         string
		uri          string
		wantPath     string
		wantCategory string
		wantStatus   int
	}{
		{name: "asset", uri: testOrigin + "/style.css?v=1", wantPath: "style.css", wantCategory: "asset"},
		{name: "root", uri: testOrigin + "/", wantPath: "index.html", wantCategory: "asset"},
		{name: "wrong host", uri: "https://example.test/index.html", wantPath: "wrong_host", wantCategory: "wrong_host", wantStatus: http.StatusForbidden},
		{name: "wrong scheme", uri: "http://" + testVirtualHost + "/index.html", wantPath: "wrong_scheme", wantCategory: "wrong_scheme", wantStatus: http.StatusForbidden},
		{name: "traversal", uri: testOrigin + "/../secret", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "invalid", uri: "://", wantPath: "invalid", wantCategory: "invalid", wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotStatus := resolveAssetRequest(testVirtualHost, test.uri)
			if got.path != test.wantPath || got.category != test.wantCategory || gotStatus != test.wantStatus {
				t.Fatalf("resolveAssetRequest() = {%q %q}, %d, want {%q %q}, %d", got.path, got.category, gotStatus, test.wantPath, test.wantCategory, test.wantStatus)
			}
		})
	}
}

func TestAssetProviderResolveDiagnosticCategories(t *testing.T) {
	provider := newTestAssetProvider(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
		"style.css":  &fstest.MapFile{Data: []byte("body{}")},
	})
	tests := []struct {
		name         string
		uri          string
		wantPath     string
		wantCategory string
		wantStatus   int
	}{
		{name: "asset", uri: testOrigin + "/style.css?v=1", wantPath: "style.css", wantCategory: "asset", wantStatus: http.StatusOK},
		{name: "favicon", uri: testOrigin + "/favicon.ico", wantPath: "favicon.ico", wantCategory: "favicon", wantStatus: http.StatusNoContent},
		{name: "missing", uri: testOrigin + "/missing.js", wantPath: "missing.js", wantCategory: "missing", wantStatus: http.StatusNotFound},
		{name: "wrong host", uri: "https://example.test/index.html", wantPath: "wrong_host", wantCategory: "wrong_host", wantStatus: http.StatusForbidden},
		{name: "wrong scheme", uri: "http://" + testVirtualHost + "/index.html", wantPath: "wrong_scheme", wantCategory: "wrong_scheme", wantStatus: http.StatusForbidden},
		{name: "traversal", uri: testOrigin + "/../secret", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "invalid", uri: "://", wantPath: "invalid", wantCategory: "invalid", wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := provider.resolve(test.uri)
			if response.request.path != test.wantPath || response.request.category != test.wantCategory || response.status != test.wantStatus {
				t.Fatalf("resolve() diagnostic = {%q %q %d}, want {%q %q %d}", response.request.path, response.request.category, response.status, test.wantPath, test.wantCategory, test.wantStatus)
			}
		})
	}
}

func TestAssetProviderResolveReadError(t *testing.T) {
	provider := newTestAssetProvider(errorFS{})
	response := provider.resolve(testOrigin + "/index.html")
	if response.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.status, http.StatusInternalServerError)
	}
	if response.request.category != "read_error" {
		t.Fatalf("category = %q, want read_error", response.request.category)
	}
}

func containsHeader(headers, prefix string) bool {
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

type errorFS struct{}

func (errorFS) Open(string) (fs.File, error) {
	return nil, errAssetTestRead
}

var errAssetTestRead = fs.ErrInvalid
