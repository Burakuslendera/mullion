//go:build windows

package host

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
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
		{name: "backslash traversal (%5c)", uri: testOrigin + "/..%5c..%5csecret", wantErr: http.StatusForbidden},
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
			if !containsHeader(response.headers, "X-Content-Type-Options: nosniff") {
				t.Fatalf("headers = %q, want nosniff", response.headers)
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
		{name: "backslash traversal (%5c)", uri: testOrigin + "/..%5c..%5csecret", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		// The control-byte, colon, dot-normalisation and invalid-UTF-8 rejects of
		// containsBackslashColonOrControl, hasTraversalSegment and the fs.ValidPath
		// gate (issues #31, #66). url.Parse decodes a percent-encoded byte to a
		// literal one in Path and path.Clean is lexical, so without these the byte
		// reaches fs.ReadFile and the boundary would lean on the OS or the fs.FS.
		{name: "null byte (%00)", uri: testOrigin + "/a%00b", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "escape byte (%1b)", uri: testOrigin + "/a%1bb.css", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "delete byte (%7f)", uri: testOrigin + "/a%7fb", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		// Valid-UTF-8 C1 is caught by the rune check; a raw lone C1 byte decodes to
		// U+FFFD and passes it, so the fs.ValidPath gate (invalid UTF-8) catches it.
		{name: "c1 byte, valid utf-8 (%c2%85)", uri: testOrigin + "/a%c2%85b.css", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "raw invalid byte (%85)", uri: testOrigin + "/a%85b.css", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "trailing-space dotdot (%20)", uri: testOrigin + "/..%20/secret.txt", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "triple-dot segment", uri: testOrigin + "/.../secret", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "colon drive/ADS (%3a)", uri: testOrigin + "/file.txt%3astream", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
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

// TestResolveAssetRequestServesNonASCIIName proves the C1-control reject in
// containsBackslashColonOrControl (issue #66) ranges over runes, not bytes: a
// legitimate multi-byte UTF-8 asset name is served even though its UTF-8
// continuation bytes (here 0x97 and 0x9c) fall inside the 0x80-0x9f C1 range at
// the byte level. A byte-level check would reject this name; a rune-level one
// must not, which is why the check iterates runes.
func TestResolveAssetRequestServesNonASCIIName(t *testing.T) {
	// A two-character CJK name (U+65E5 U+672C) plus ".html", built from runes so
	// this source stays ASCII, requested percent-encoded as its UTF-8 bytes.
	want := string(rune(0x65e5)) + string(rune(0x672c)) + ".html"
	got, status := resolveAssetRequest(testVirtualHost, testOrigin+"/%e6%97%a5%e6%9c%ac.html")
	if got.path != want || got.category != "asset" || status != 0 {
		t.Fatalf("resolveAssetRequest() = {%q %q}, %d, want {%q %q}, 0", got.path, got.category, status, want, "asset")
	}
}

// TestAssetBoundaryOSDirFSDoesNotEscape pins the load-bearing OS assumption behind
// the filter (issue #66): even if a trailing-dot/space ".." reached
// fs.ReadFile(os.DirFS(root), ...) - which resolveAssetRequest now rejects itself
// - the OS must not normalise ".. ", "...", ".. ." into ".." and walk out of the
// root. This is the headless equivalent of the issue's live probe; a regression in
// Go's os.DirFS, or a Windows build that collapses these, fails here rather than
// silently opening the asset boundary.
func TestAssetBoundaryOSDirFSDoesNotEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "webroot")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir web root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	// Planted as a sibling of the web root: reachable only by escaping it.
	if err := os.WriteFile(filepath.Join(base, "secret.txt"), []byte("SECRET"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	dirFS := os.DirFS(root)
	if _, err := fs.ReadFile(dirFS, "index.html"); err != nil {
		t.Fatalf("index.html should read from inside the web root: %v", err)
	}
	for _, escape := range []string{"../secret.txt", ".. /secret.txt", ".../secret.txt", ".. ./secret.txt"} {
		if data, err := fs.ReadFile(dirFS, escape); err == nil {
			t.Fatalf("os.DirFS escaped the web root via %q: read %q", escape, data)
		}
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
		{name: "backslash traversal (%5c)", uri: testOrigin + "/..%5c..%5csecret", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		// The control-byte, colon, dot-normalisation and invalid-UTF-8 rejects of
		// containsBackslashColonOrControl, hasTraversalSegment and the fs.ValidPath
		// gate (issues #31, #66). url.Parse decodes a percent-encoded byte to a
		// literal one in Path and path.Clean is lexical, so without these the byte
		// reaches fs.ReadFile and the boundary would lean on the OS or the fs.FS.
		{name: "null byte (%00)", uri: testOrigin + "/a%00b", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "escape byte (%1b)", uri: testOrigin + "/a%1bb.css", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "delete byte (%7f)", uri: testOrigin + "/a%7fb", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		// Valid-UTF-8 C1 is caught by the rune check; a raw lone C1 byte decodes to
		// U+FFFD and passes it, so the fs.ValidPath gate (invalid UTF-8) catches it.
		{name: "c1 byte, valid utf-8 (%c2%85)", uri: testOrigin + "/a%c2%85b.css", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "raw invalid byte (%85)", uri: testOrigin + "/a%85b.css", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "trailing-space dotdot (%20)", uri: testOrigin + "/..%20/secret.txt", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "triple-dot segment", uri: testOrigin + "/.../secret", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "colon drive/ADS (%3a)", uri: testOrigin + "/file.txt%3astream", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
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
