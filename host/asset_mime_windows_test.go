//go:build windows

package host

import "testing"

// contentTypeForAsset decides how the browser interprets the bytes an asset
// serves, so its extension mapping is a small security surface: the wrong type
// can turn inert data into executable script in the mullion.localhost origin.
// The hardcoded switch and the unknown-extension fallback are deterministic and
// locked here; the mime.TypeByExtension middle branch reads the machine's
// registry MIME table and is deliberately left unpinned.
//
// The function takes no content. Until issue #100 it fell back to
// http.DetectContentType, and this table asserted that a name with no extension
// carrying HTML bytes was answered text/html - the exact outcome the comment
// above says must not happen, and the one the response's own nosniff header
// then made irreversible.
func TestContentTypeForAsset(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"html", "index.html", "text/html; charset=utf-8"},
		{"css", "assets/style.css", "text/css; charset=utf-8"},
		{"js", "app.js", "text/javascript; charset=utf-8"},
		{"json", "data.json", "application/json; charset=utf-8"},
		{"svg", "icon.svg", "image/svg+xml"},
		{"png", "logo.png", "image/png"},
		{"ico", "favicon.ico", "image/x-icon"},
		{"uppercase extension", "INDEX.HTML", "text/html; charset=utf-8"},
		{"mixed case", "Style.Css", "text/css; charset=utf-8"},
		// These rows pin the contract, not the layer that satisfies it, and for
		// most of them the layer underneath is not the machine's registry: Go's
		// mime package compiles its own table in and the registry can only add to
		// it. Measured, mime.TypeByExtension answers for every extension below
		// except ".woff" and ".woff2", so deleting those cases from the switch
		// still passes the suite while deleting the woff ones does not. What no
		// headless test here can stand in for is a *future* Go whose built-in
		// table changes an answer; that is what these rows are insurance against
		// (decisions/0031).
		{"htm", "index.htm", "text/html; charset=utf-8"},
		{"txt", "notes.txt", "text/plain; charset=utf-8"},
		{"mjs", "module.mjs", "text/javascript; charset=utf-8"},
		{"jpg", "photo.jpg", "image/jpeg"},
		{"jpeg", "photo.jpeg", "image/jpeg"},
		{"gif", "anim.gif", "image/gif"},
		{"webp", "photo.webp", "image/webp"},
		// The only two rows below that the switch actually holds up, so the only
		// two whose deletion this table catches. ".woff" was missing until an
		// audit measured the pair: deleting both cases failed on woff2 alone, so
		// ".woff" was locked by nothing.
		{"woff", "font.woff", "font/woff"},
		{"woff2", "font.woff2", "font/woff2"},
		// The uppercase rows further up do not lock strings.ToLower: measured,
		// mime.TypeByExtension answers ".HTML" and ".Css" case-insensitively, so
		// deleting the fold left them passing. It answers "" for every spelling of
		// ".woff" and ".woff2", so these two are the only names whose type depends
		// on the fold, and a mutant that removed it survived the whole suite until
		// they were added.
		{"uppercase woff", "FONT.WOFF", "font/woff"},
		{"mixed case woff2", "Font.Woff2", "font/woff2"},
		{"wasm", "app.wasm", "application/wasm"},
		{"no extension", "README", "application/octet-stream"},
		{"unknown extension", "upload.foobar", "application/octet-stream"},
		{"content-addressed name", "uploads/9f0c4a1b2e", "application/octet-stream"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := contentTypeForAsset(test.path); got != test.want {
				t.Fatalf("contentTypeForAsset(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

// The property behind the table above, stated directly: the bytes never decide.
// Same HTML payload under three names; only the one that says .html is html.
func TestContentTypeForAssetIgnoresTheBytes(t *testing.T) {
	for _, path := range []string{"README", "upload.foobar", "uploads/9f0c4a1b2e"} {
		if got := contentTypeForAsset(path); got == "text/html; charset=utf-8" {
			t.Fatalf("contentTypeForAsset(%q) = %q, want anything but html", path, got)
		}
	}
	if got := contentTypeForAsset("page.html"); got != "text/html; charset=utf-8" {
		t.Fatalf("contentTypeForAsset(page.html) = %q, want html", got)
	}
}
