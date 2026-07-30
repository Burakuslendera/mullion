//go:build windows

package host

import (
	"mime"
	"path/filepath"
	"strings"
)

// contentTypeForAsset maps an asset name to a content type. It never inspects
// the bytes: mullion serves the type the application's own naming implies, and
// a name mullion cannot classify is served as an opaque download rather than
// guessed at.
//
// Sniffing here would defeat the nosniff header the same response carries.
// http.DetectContentType answers text/html for anything opening with a tag, so
// an upload directory, a content-addressed blob store, or any name without a
// trusted extension would be stamped text/html on bytes nobody classified - and
// nosniff would then make that label irreversible, giving HTML execution in the
// origin the bridge is injected into (issue #100).
//
// The mime.TypeByExtension branch is deliberately left unpinned. It is still a
// decision about the *name*, which is the model here; it is not a decision about
// the bytes.
//
// Be precise about what that branch is, because the obvious reading is wrong.
// It is not "the machine's registry". Go's mime package compiles a table into
// the binary and installs it first, then lets the Windows registry scan add to
// it: initMime calls setMimeTypes(builtinTypesLower, ...) before osInitMime, and
// the registry path only ever calls setExtensionType, which Stores and never
// deletes. So a registry entry can override or extend an answer; it can never
// remove one. Measured: ".shtml" has a registry key with no Content Type value
// and ".ehtml" has no key at all, and both still answer text/html.
//
// This branch is therefore not a guarantee of "never html", and the reason is
// stronger than machine variance: Go's own built-in table maps ".htm", ".shtml"
// and ".ehtml" to text/html on every machine. The promise this function keeps is
// that mullion decides from the name; it is not that html can only come from
// ".html". decisions/0031 records what that costs.
func contentTypeForAsset(assetPath string) string {
	switch strings.ToLower(filepath.Ext(assetPath)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	// Everything below is named for one of two reasons, and it is worth knowing
	// which, because an audit measured the rationale first written here to be
	// wrong. It said these rows insure the common web types against a machine
	// with a pruned registry MIME table; no such machine exists, since Go's
	// built-in table is compiled in and the registry cannot subtract from it.
	//
	// Measured, mime.TypeByExtension already answers for ".htm", ".txt", ".mjs",
	// ".jpg", ".jpeg", ".gif", ".webp" and ".wasm" with no registry help at all.
	// Those rows are redundant and kept anyway: they make the type an asset gets
	// readable here rather than in another package's table, which is the whole
	// point of deciding from the name. Only ".woff" and ".woff2" are load-bearing
	// - mime.TypeByExtension answers "" for both, so without these two rows a
	// font would be served application/octet-stream.
	case ".htm":
		return "text/html; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".mjs":
		return "text/javascript; charset=utf-8"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".wasm":
		return "application/wasm"
	}
	if contentType := mime.TypeByExtension(filepath.Ext(assetPath)); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}
