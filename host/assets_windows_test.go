//go:build windows

package host

import (
	"io/fs"
	"net/http"
	"os"
	"os/exec"
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
			request, gotErr := resolveAssetRequest(testVirtualHost, test.uri)
			got := ""
			if gotErr == 0 {
				got = request.path
			}
			if got != test.want || gotErr != test.wantErr {
				t.Fatalf("resolveAssetRequest() = %q, %d, want %q, %d", got, gotErr, test.want, test.wantErr)
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
		// "/." is refused, not folded to the root. resolveAssetRequest used to carry
		// a cleanPath == "/." arm next to the "/" one; it was unreachable twice over
		// - hasTraversalSegment refuses a "." segment before Clean runs, and Clean on
		// a "/"-prefixed input never returns "/." anyway - and a mutant that deleted
		// it survived the suite. This row records where "/." actually lands.
		{name: "dot root", uri: testOrigin + "/.", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
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
		// An ordinary name growing a trailing dot or space is an alias for the name
		// without it, so the file the OS opens is not the name mullion classified:
		// filepath.Ext("notes.txt.") is ".", the extension switch misses, and the
		// type came from the fallback (issue #100). #66 covered this normalisation
		// only for names that could collapse to "..". Measured over os.DirFS before
		// the fix: "notes.txt" answered text/plain and every row below answered
		// text/html on byte-identical content.
		{name: "trailing dot alias", uri: testOrigin + "/notes.txt.", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "trailing dot alias (%2e)", uri: testOrigin + "/notes.txt%2e", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "trailing space alias (%20)", uri: testOrigin + "/notes.txt%20", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "trailing dot on a directory", uri: testOrigin + "/sub./notes.txt", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		// Windows device names pass this boundary and are handed to the caller's
		// fs.FS. Deliberate, and these rows keep it that way - see decisions/0031
		// before "hardening" it. os.DirFS refuses them itself (measured on
		// go1.22.12, go1.23.12 and go1.26.5) and an embed.FS never reaches the OS,
		// so the only caller a check here would help is one who wrote a
		// passthrough fs.FS.
		{name: "device name is not rejected here", uri: testOrigin + "/nul", wantPath: "nul", wantCategory: "asset"},
		{name: "device name with an extension", uri: testOrigin + "/nul.txt", wantPath: "nul.txt", wantCategory: "asset"},
		{name: "device name in a subdirectory", uri: testOrigin + "/assets/con", wantPath: "assets/con", wantCategory: "asset"},
		{name: "device name uppercase", uri: testOrigin + "/COM1", wantPath: "COM1", wantCategory: "asset"},
		{name: "name beginning with a device name", uri: testOrigin + "/console.js", wantPath: "console.js", wantCategory: "asset"},
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

// TestAssetBoundaryOSDirFSDoesNotEscapeViaDotOrSpaceForms pins the load-bearing OS assumption behind
// the filter (issue #66): even if a trailing-dot/space ".." reached
// fs.ReadFile(os.DirFS(root), ...) - which resolveAssetRequest now rejects itself
// - the OS must not normalise ".. ", "...", ".. ." into ".." and walk out of the
// root. This is the headless equivalent of the issue's live probe; a regression in
// Go's os.DirFS, or a Windows build that collapses these, fails here rather than
// silently opening the asset boundary.
//
// The name says "dot or space forms" because issue #103 caught an earlier one,
// TestAssetBoundaryOSDirFSDoesNotEscape, claiming the whole escape class while
// proving one member of it. What is proved here is the *lexical* forms only. The
// other member of the class, a reparse point, is a different mechanism and has
// its own test below.
func TestAssetBoundaryOSDirFSDoesNotEscapeViaDotOrSpaceForms(t *testing.T) {
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

// TestAssetRootRefusesAReparsePointAndOSDirFSDoesNot is the second half of issue
// #103, and it pins a difference between two standard-library file systems rather
// than anything mullion computes. A directory junction inside the asset root
// points outside it. No name check can see that - the name is ordinary and the
// redirection lives in the filesystem - so the boundary cannot help, and this is
// why decision 0033 moved the supported Go floor to 1.24 and made
// os.OpenRoot(dir).FS() the documented way to serve assets from a directory.
//
// Both halves are asserted, because the recommendation is only worth making while
// the difference holds: os.DirFS follows the junction and *os.rootFS refuses it.
// If a future Go hardened os.DirFS the recommendation would be redundant, and if
// a future Go loosened os.Root it would be wrong. Either way this test says so.
//
// mklink /J needs no elevation, unlike a directory symlink. Where it is
// unavailable anyway - a filesystem without reparse points, a locked-down build
// agent - the test skips rather than passing vacuously.
func TestAssetRootRefusesAReparsePointAndOSDirFSDoesNot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "webroot")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{root, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("SECRET"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	junction := filepath.Join(root, "escape")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, outside).CombinedOutput(); err != nil {
		t.Skipf("mklink /J unavailable, cannot plant a reparse point: %v: %s", err, output)
	}

	// The gap, still present and asserted so the reason for 0033 stays visible.
	if data, err := fs.ReadFile(os.DirFS(root), "escape/secret.txt"); err != nil {
		t.Fatalf("os.DirFS was expected to follow the junction, and did not: %v", err)
	} else if string(data) != "SECRET" {
		t.Fatalf("os.DirFS read %q through the junction, want %q", data, "SECRET")
	}

	// The recommendation, and what it buys.
	handle, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("os.OpenRoot(%q): %v", root, err)
	}
	defer handle.Close()
	rootFS := handle.FS()

	if data, err := fs.ReadFile(rootFS, "escape/secret.txt"); err == nil {
		t.Fatalf("os.Root followed the junction and read %q: the floor move bought nothing", data)
	}
	if _, err := fs.ReadFile(rootFS, "index.html"); err != nil {
		t.Fatalf("os.Root refused a legitimate asset inside the root: %v", err)
	}

	// And the whole boundary over it: an ordinary asset still serves, the escape
	// does not, and the escape is a read error rather than a traversal reject -
	// the name was fine, the filesystem said no.
	provider := newTestAssetProvider(rootFS)
	if response := provider.resolve(testOrigin + "/index.html"); response.status != http.StatusOK {
		t.Fatalf("index.html over os.Root = %d, want 200", response.status)
	}
	response := provider.resolve(testOrigin + "/escape/secret.txt")
	if response.status == http.StatusOK {
		t.Fatalf("escape/secret.txt over os.Root = 200, body %q", response.body)
	}
	if response.request.category == "traversal" {
		t.Fatalf("escape/secret.txt category = %q, want the fs.FS refusal rather than a name reject", response.request.category)
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
		{name: "trailing dot alias", uri: testOrigin + "/style.css.", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "device name reaches the fs.FS", uri: testOrigin + "/nul", wantPath: "nul", wantCategory: "missing", wantStatus: http.StatusNotFound},
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

// TestAssetResponseNeverTypesUnclassifiedBytesAsHTML is issue #100's measured
// table, inverted into a guard. The response carries nosniff, which makes the
// content type mullion chooses irreversible - so a type mullion guessed from the
// bytes is worse than no type at all. Two ways it used to guess, both over an
// fs.FS backed by the real filesystem, both on byte-identical content:
//
//	before: uploads/note.txt -> text/plain      notes.txt  -> text/plain
//	        uploads/abc123   -> text/html       notes.txt. -> text/html
//	        uploads/x.foobar -> text/html       data.json. -> text/html
//
// An application serving an upload directory or a content-addressed blob store
// got HTML execution in the origin the bridge is injected into. The payload here
// opens with a script tag, which is what http.DetectContentType keyed on.
func TestAssetResponseNeverTypesUnclassifiedBytesAsHTML(t *testing.T) {
	payload := []byte(`<script>window.pwned=1</script>`)
	dir := t.TempDir()
	for _, name := range []string{"note.txt", "abc123", "x.foobar", "notes.txt", "data.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), payload, 0o644); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}
	provider := newTestAssetProvider(os.DirFS(dir))

	// Served, but never as html: the name carries no extension mullion trusts.
	for _, name := range []string{"abc123", "x.foobar"} {
		response := provider.resolve(testOrigin + "/" + name)
		if response.status != http.StatusOK {
			t.Fatalf("%q status = %d, want 200", name, response.status)
		}
		if response.contentType != "application/octet-stream" {
			t.Fatalf("%q content type = %q, want application/octet-stream", name, response.contentType)
		}
	}
	// Refused at the boundary: the trailing dot or space is an alias, so the name
	// mullion classified is not the file the OS would open.
	for _, name := range []string{"notes.txt.", "notes.txt%20", "notes.txt%2e", "data.json."} {
		response := provider.resolve(testOrigin + "/" + name)
		if response.status != http.StatusForbidden {
			t.Fatalf("%q status = %d, want 403", name, response.status)
		}
		if response.contentType == "text/html; charset=utf-8" {
			t.Fatalf("%q content type = %q, want anything but html", name, response.contentType)
		}
	}
	// The control: a name that does say .txt is still typed from its extension.
	response := provider.resolve(testOrigin + "/note.txt")
	if response.status != http.StatusOK || response.contentType != "text/plain; charset=utf-8" {
		t.Fatalf("note.txt = {%d %q}, want {200 text/plain; charset=utf-8}", response.status, response.contentType)
	}
}

// TestAssetBoundaryDoesNotFilterDeviceNames locks a decision, not a defence:
// Windows device names are handed to the caller's fs.FS rather than refused here
// (decisions/0031). It is written as a guard because "the asset boundary should
// reject CON and NUL" is an easy and plausible-sounding change to propose, and
// this repository had it implemented before it was measured and removed.
//
// Why it is not needed, measured: os.DirFS refuses the bare names itself, on
// go1.22 already - dirFS.join -> safefilepath.FromFS -> IsReservedName, renamed
// to filepathlite.Localize in 1.23 without a behaviour change, identical on
// go1.22.12, go1.23.12 and go1.26.5. An embed.FS never reaches the OS at all, so
// nothing there can resolve to a device. That leaves a caller who wrote their own
// passthrough fs.FS, whose own code is where the check belongs.
//
// What the removal costs, also measured: through a passthrough fs.FS over
// os.Open, ReadFile("nul") returns 0 bytes and a nil error, so such a caller
// answers 200 with an empty body for /nul. The request path is chosen by the
// page, so no application has to "use" a device name for that to be reachable.
// The cost is accepted; if it ever bites, decisions/0031 says what to change.
func TestAssetBoundaryDoesNotFilterDeviceNames(t *testing.T) {
	provider := newTestAssetProvider(fstest.MapFS{
		"nul":        &fstest.MapFile{Data: []byte("not a device here")},
		"console.js": &fstest.MapFile{Data: []byte("window.x={}")},
	})
	names := []string{
		"nul", "con", "aux", "prn", "com1", "lpt1", "conin$", "conout$",
		"NUL", "Con", "AUX", "con/app.js", "nul/style.css",
		"nul.txt", "CON.TXT", "aux.min.js", "con.json", "prn.woff2", "com1.map",
		"constants.js", "auxiliary.css", "com.js", "printer.png",
		"com10", "conin", "clock$", "console.js",
	}
	// Superscript COM/LPT are devices on Windows too - syscall.FullPath answers
	// \\.\com<superscript-one> - and are not filtered here either. Built from
	// runes because TestNoNonASCIIInSource keeps this source ASCII.
	for _, superscript := range []rune{0x00b9, 0x00b2, 0x00b3} {
		names = append(names, "com"+string(superscript), "lpt"+string(superscript))
	}
	for _, name := range names {
		request, status := resolveAssetRequest(testVirtualHost, testOrigin+"/"+name)
		if status != 0 || request.category != "asset" {
			t.Fatalf("%q = {%q %q}, %d, want it handed on as an asset", name, request.path, request.category, status)
		}
	}
	// The one served fixture reaches the fs.FS and comes back as content, which
	// is the whole point: the boundary does not stand between them.
	if response := provider.resolve(testOrigin + "/nul"); response.status != http.StatusOK {
		t.Fatalf("nul status = %d, want 200 from the fs.FS", response.status)
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
