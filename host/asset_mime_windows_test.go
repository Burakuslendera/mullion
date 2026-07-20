//go:build windows

package host

import "testing"

// contentTypeForAsset decides how the browser interprets the bytes an asset
// serves, so its extension mapping is a small security surface: the wrong type
// can turn inert data into executable script in the mullion.local origin. The
// hardcoded switch and the http.DetectContentType fallback are deterministic
// and locked here; the mime.TypeByExtension middle branch reads the machine's
// registry MIME table and is deliberately left unpinned.
func TestContentTypeForAsset(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		content []byte
		want    string
	}{
		{"html", "index.html", nil, "text/html; charset=utf-8"},
		{"css", "assets/style.css", nil, "text/css; charset=utf-8"},
		{"js", "app.js", nil, "text/javascript; charset=utf-8"},
		{"json", "data.json", nil, "application/json; charset=utf-8"},
		{"svg", "icon.svg", nil, "image/svg+xml"},
		{"png", "logo.png", nil, "image/png"},
		{"ico", "favicon.ico", nil, "image/x-icon"},
		{"uppercase extension", "INDEX.HTML", nil, "text/html; charset=utf-8"},
		{"mixed case", "Style.Css", nil, "text/css; charset=utf-8"},
		{"no extension, html content sniffs to html", "README", []byte("<!doctype html><html></html>"), "text/html; charset=utf-8"},
		{"no extension, binary content sniffs to octet-stream", "blob", []byte{0x00, 0x01, 0x02, 0x03}, "application/octet-stream"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := contentTypeForAsset(test.path, test.content); got != test.want {
				t.Fatalf("contentTypeForAsset(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}
