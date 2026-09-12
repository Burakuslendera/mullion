//go:build windows

package host

import (
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"unsafe"
)

const testVirtualHost = defaultVirtualHost

var (
	testOrigin      = "https://" + testVirtualHost
	testAssetOrigin = newCanonicalOrigin("https", testVirtualHost, "")
)

func newTestAssetProvider(assets fs.FS) assetProvider {
	return newAssetProvider(assets, newLogSink(NopLogger{}), testAssetOrigin, newNativeDiagnostics())
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
		{name: "8.3 alias spelling", uri: testOrigin + "/PAYLOA~1.HTM", wantErr: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, gotErr := resolveAssetRequest(testAssetOrigin, test.uri)
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

func TestResolveAssetRequestUsesCanonicalOriginSemantics(t *testing.T) {
	custom := newCanonicalOrigin("https", "example.internal", "")
	for _, raw := range []string{
		"https://example.internal/index.html",
		"https://EXAMPLE.INTERNAL/index.html",
		"https://example.internal:443/index.html",
		"https://example.internal:0443/index.html",
	} {
		got, status := resolveAssetRequest(custom, raw)
		if status != 0 || got.path != "index.html" {
			t.Fatalf("%q rejected: %q, %d", raw, got.path, status)
		}
	}
	for _, raw := range []string{
		"https://example.internal:444/index.html",
		testOrigin + "/index.html",
		"https://user:secret@example.internal/index.html",
	} {
		if _, status := resolveAssetRequest(custom, raw); status != http.StatusForbidden {
			t.Fatalf("%q accepted under custom origin: %d", raw, status)
		}
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

func TestAssetProviderResolveDirectoryIsMissingWithoutSessionError(t *testing.T) {
	logger := newLogSink(NopLogger{})
	provider := newAssetProvider(fstest.MapFS{
		"sub/index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}, logger, testAssetOrigin, newNativeDiagnostics())

	response := provider.resolve(testOrigin + "/sub")
	if response.status != http.StatusNotFound {
		t.Fatalf("directory status = %d, want 404", response.status)
	}
	if response.request.category != "missing" {
		t.Fatalf("directory category = %q, want missing", response.request.category)
	}
	if string(response.body) != http.StatusText(http.StatusNotFound) {
		t.Fatalf("directory body = %q, want %q", response.body, http.StatusText(http.StatusNotFound))
	}

	// This is the same error-level decision made by webResourceRequested after
	// resolving a response. A renderer-chosen directory request must not raise
	// SessionErrorCount; it is an ordinary missing asset and is warn-level.
	for range 1000 {
		provider.logAssetResponseError(response)
	}
	if got := logger.ErrorCount(); got != 0 {
		t.Fatalf("directory request error count = %d, want 0", got)
	}
	if got := logger.WarnCount(); got != 1000 {
		t.Fatalf("directory request warn count = %d, want 1000", got)
	}
}

func TestAssetProviderResolveServesRealFavicon(t *testing.T) {
	const faviconBody = "real favicon"
	provider := newTestAssetProvider(fstest.MapFS{
		"favicon.ico": &fstest.MapFile{Data: []byte(faviconBody)},
	})

	response := provider.resolve(testOrigin + "/favicon.ico")
	if response.status != http.StatusOK {
		t.Fatalf("real favicon status = %d, want 200", response.status)
	}
	if response.request.category != "asset" {
		t.Fatalf("real favicon category = %q, want asset", response.request.category)
	}
	if response.contentType != "image/x-icon" {
		t.Fatalf("real favicon content type = %q, want image/x-icon", response.contentType)
	}
	if string(response.body) != faviconBody {
		t.Fatalf("real favicon body = %q, want %q", response.body, faviconBody)
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
		{name: "zero-width space (%e2%80%8b)", uri: testOrigin + "/a%e2%80%8bb.css", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "bidi override (%e2%80%ae)", uri: testOrigin + "/a%e2%80%aeb.css", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
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
		// The 8.3 short-name shape (issue #139). The generated extension is the
		// long one truncated to three characters, so "PAYLOA~1.HTM" is html where
		// "payload.htmlx" is opaque: the alias spelling is refused instead of
		// typed. Names that only resemble the shape are served, because refusing
		// more than the grammar expresses would widen the availability cost.
		{name: "8.3 alias spelling", uri: testOrigin + "/PAYLOA~1.HTM", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "8.3 alias spelling (%7e)", uri: testOrigin + "/payloa%7e1.htm", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "8.3 alias spelling without an extension", uri: testOrigin + "/report~1", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
		{name: "short-shaped name that is not generated", uri: testOrigin + "/report~1.backup", wantPath: "report~1.backup", wantCategory: "asset"},
		{name: "tilde without a serial is served", uri: testOrigin + "/a~b.txt", wantPath: "a~b.txt", wantCategory: "asset"},
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
			got, gotStatus := resolveAssetRequest(testAssetOrigin, test.uri)
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
	got, status := resolveAssetRequest(testAssetOrigin, testOrigin+"/%e6%97%a5%e6%9c%ac.html")
	if got.path != want || got.category != "asset" || status != 0 {
		t.Fatalf("resolveAssetRequest() = {%q %q}, %d, want {%q %q}, 0", got.path, got.category, status, want, "asset")
	}
}

func TestIs8Dot3AliasSegment(t *testing.T) {
	// The multibyte rows are composed from runes so this source stays ASCII,
	// the discipline TestNoNonASCIIInSource enforces. Each is a shape NTFS
	// sizes in characters whose UTF-8 byte length exceeds the limit the shape
	// allows, so a byte count would have admitted it to the classifier.
	runes := func(values ...rune) string { return string(values) }
	six := runes(0x0410, 0x0411, 0x0412, 0x0413, 0x0414, 0x0415)
	eight := runes(0x0410, 0x0411, 0x0412, 0x0413, 0x0414, 0x0415, 0x0416, 0x0417)
	ext := runes(0x042f, 0x0417, 0x042b)
	tests := []struct {
		name    string
		segment string
		want    bool
	}{
		{name: "generated short name with extension", segment: "PAYLOA~1.HTM", want: true},
		{name: "lowercase spelling of one", segment: "payloa~1.htm", want: true},
		{name: "without an extension", segment: "REPORT~1", want: true},
		{name: "two-digit serial", segment: "AB~12.C", want: true},
		{name: "eight-character whole base", segment: "ABCDEF~9", want: true},
		{name: "multibyte base at eight characters", segment: six + "~1.HTM", want: true},
		{name: "multibyte extension at three characters", segment: runes(0x0410, 0x0411, 0x0412) + "~1." + ext, want: true},
		{name: "multibyte base beyond eight characters", segment: eight + "~1.HTM", want: false},
		{name: "serial too large for the shape", segment: "LONGNA~999.HTM", want: false},
		{name: "tilde without a serial", segment: "report~.txt", want: false},
		{name: "tilde followed by letters", segment: "a~b.txt", want: false},
		{name: "no tilde", segment: "notes.txt", want: false},
		{name: "extension longer than three characters", segment: "report~1.backup", want: false},
		{name: "no base before the serial", segment: "~1.htm", want: false},
		{name: "dot before the serial", segment: "a.b~1.htm", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := is8Dot3AliasSegment(test.segment); got != test.want {
				t.Fatalf("is8Dot3AliasSegment(%q) = %v, want %v", test.segment, got, test.want)
			}
		})
	}
}

// windowsShortPath returns the 8.3 short name Windows reports for path. It
// skips rather than fails when none exists: 8.3 generation is a per-volume
// setting, so the alias arm of a test is a volume property, not an assertion.
func windowsShortPath(t *testing.T, path string) string {
	t.Helper()
	getShortPathName := syscall.NewLazyDLL("kernel32.dll").NewProc("GetShortPathNameW")
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Skipf("path cannot be passed to GetShortPathNameW: %v", err)
	}
	size, _, _ := getShortPathName.Call(uintptr(unsafe.Pointer(pointer)), 0, 0)
	if size == 0 {
		t.Skipf("GetShortPathNameW produced no short path for %q", path)
	}
	buffer := make([]uint16, size)
	written, _, _ := getShortPathName.Call(uintptr(unsafe.Pointer(pointer)), uintptr(unsafe.Pointer(&buffer[0])), size)
	if written == 0 || written > size {
		t.Skipf("GetShortPathNameW could not report the short path for %q", path)
	}
	return syscall.UTF16ToString(buffer)
}

func openRootAssetFS(t *testing.T, dir string) fs.FS {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("os.OpenRoot(%q): %v", dir, err)
	}
	t.Cleanup(func() { root.Close() })
	return root.FS()
}

// TestAssetProviderNeverTypesAnAliasSpellingOfAnOpaqueName pins the boundary
// against issue #139. The long name "payload.htmlx" is opaque - the extension
// switch misses it - while the 8.3 short name Windows generates for the same
// file ends in ".HTM", the truncated extension, which the switch answers html.
// The alias spelling is refused before the classifier can see it, so both
// spellings of one file agree on its type by construction.
func TestAssetProviderNeverTypesAnAliasSpellingOfAnOpaqueName(t *testing.T) {
	dir := t.TempDir()
	const longName = "payload.htmlx"
	body := []byte("inert bytes, not markup")
	for _, name := range []string{longName, "plain.txt", "data1234"} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}
	alias := filepath.Base(windowsShortPath(t, filepath.Join(dir, longName)))
	if alias == longName {
		t.Skipf("this volume generated no distinct 8.3 short name for %q", longName)
	}
	if !is8Dot3AliasSegment(alias) {
		t.Skipf("generated short name %q is outside the shape the boundary reasons about", alias)
	}
	longInfo, err := os.Stat(filepath.Join(dir, longName))
	if err != nil {
		t.Fatalf("stat %q: %v", longName, err)
	}
	aliasInfo, err := os.Stat(filepath.Join(dir, alias))
	if err != nil {
		t.Fatalf("stat the generated short name %q: %v", alias, err)
	}
	if !os.SameFile(longInfo, aliasInfo) {
		t.Fatalf("short name %q resolves to a different file than %q", alias, longName)
	}

	for name, assets := range map[string]fs.FS{
		"os.DirFS":    os.DirFS(dir),
		"os.OpenRoot": openRootAssetFS(t, dir),
	} {
		t.Run(name, func(t *testing.T) {
			provider := newTestAssetProvider(assets)

			// The long spelling is opaque and stays opaque: the classifier sees
			// the name that was handed, and ".htmlx" is not a type it trusts.
			response := provider.resolve(testOrigin + "/" + longName)
			if response.status != http.StatusOK || response.contentType != "application/octet-stream" {
				t.Fatalf("%q = {%d %q}, want {200 application/octet-stream}", longName, response.status, response.contentType)
			}
			// The alias spelling opens this same file; the boundary refuses it
			// rather than typing it from the truncated extension.
			for _, spelling := range []string{alias, strings.ToLower(alias)} {
				response := provider.resolve(testOrigin + "/" + spelling)
				if response.status != http.StatusForbidden {
					t.Fatalf("%q = %d, want 403", spelling, response.status)
				}
				if response.contentType == "text/html; charset=utf-8" {
					t.Fatalf("%q content type = %q, want anything but html", spelling, response.contentType)
				}
			}
			// Controls: a typed name keeps its type and an untyped one stays
			// opaque by its own spelling, so the refusals above are about the
			// aliasing and not about the fixture's bytes or directory.
			response = provider.resolve(testOrigin + "/plain.txt")
			if response.status != http.StatusOK || response.contentType != "text/plain; charset=utf-8" {
				t.Fatalf("plain.txt = {%d %q}, want {200 text/plain; charset=utf-8}", response.status, response.contentType)
			}
			response = provider.resolve(testOrigin + "/data1234")
			if response.status != http.StatusOK || response.contentType != "application/octet-stream" {
				t.Fatalf("data1234 = {%d %q}, want {200 application/octet-stream}", response.status, response.contentType)
			}
		})
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
	mklink := exec.Command("cmd", "/c", "mklink", "/J", junction, outside)
	// Without this the console flashes on the developer's desktop once per run,
	// which is the seam issue #76 added and every other exec here already uses.
	hideChildConsole(mklink)
	if output, err := mklink.CombinedOutput(); err != nil {
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
		{name: "8.3 alias spelling", uri: testOrigin + "/payloa~1.htm", wantPath: "traversal", wantCategory: "traversal", wantStatus: http.StatusForbidden},
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
		request, status := resolveAssetRequest(testAssetOrigin, testOrigin+"/"+name)
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
