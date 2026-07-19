//go:build windows

package host

import (
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/Burakuslendera/mullion/internal/webview2"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

type assetProvider struct {
	assets      fs.FS
	log         *logSink
	virtualHost string
	diagnostics *nativeDiagnostics
}

type assetResponse struct {
	status      int
	reason      string
	headers     string
	contentType string
	body        []byte
	request     assetRequest
}

type assetRequest struct {
	path     string
	category string
}

func newAssetProvider(assets fs.FS, log *logSink, virtualHost string, diagnostics *nativeDiagnostics) assetProvider {
	if log == nil {
		log = newLogSink(NopLogger{})
	}
	if virtualHost == "" {
		virtualHost = defaultVirtualHost
	}
	return assetProvider{assets: assets, log: log, virtualHost: virtualHost, diagnostics: diagnostics}
}

func (provider *assetProvider) webResourceRequested(request *webview2.ICoreWebView2WebResourceRequest, args *webview2.ICoreWebView2WebResourceRequestedEventArgs, environment *webview2.ICoreWebView2Environment) {
	if request == nil {
		provider.log.Warn("mullion: asset request unavailable")
		return
	}
	if args == nil {
		provider.log.Warn("mullion: asset request args unavailable")
		return
	}
	if environment == nil {
		provider.log.Warn("mullion: asset environment unavailable")
		return
	}
	uri, err := request.GetUri()
	if err != nil {
		provider.log.Warn("mullion: asset request uri failed, reason=" + logsafe.Reason(err))
		return
	}
	method, err := request.GetMethod()
	if err != nil {
		method = "unknown"
		provider.log.Warn("mullion: asset request method failed, reason=" + logsafe.Reason(err))
	}
	response := provider.resolve(uri)
	provider.diagnostics.recordAsset(response, method)
	if response.status >= http.StatusBadRequest {
		provider.logAssetResponseError(response)
	} else {
		provider.logAssetResponseDebug(response, method)
	}
	webviewResponse, stream, err := provider.createWebResourceResponse(environment, response)
	if err != nil {
		provider.log.Error("mullion: asset response failed, reason=" + logsafe.Reason(err))
		return
	}
	// Deferred so a panic between here and the return cannot strand the two
	// owned references: the event dispatch recovers panics and keeps the
	// process alive, which would leak them for good (issue #45). The release
	// still runs after PutResponse - by then the runtime has taken its own
	// references, so ours are redundant.
	defer provider.releaseResponse(webviewResponse, stream)
	if err := args.PutResponse(webviewResponse); err != nil {
		provider.log.Error("mullion: asset response put failed, reason=" + logsafe.Reason(err))
	}
}

func (provider *assetProvider) logAssetResponseError(response assetResponse) {
	message := "mullion: asset response error, status=" + logsafe.Message(response.reason) +
		", category=" + logsafe.Message(response.request.category) +
		", asset=" + logsafe.Message(response.request.path)
	if response.status >= http.StatusInternalServerError {
		provider.log.Error(message)
		return
	}
	provider.log.Warn(message)
}

func (provider *assetProvider) resolve(rawURI string) assetResponse {
	request, status := resolveAssetRequest(provider.virtualHost, rawURI)
	if status != 0 {
		return errorAssetResponse(status, request)
	}
	assetPath := request.path
	if assetPath == "favicon.ico" {
		// Answer the browser's unsolicited favicon probe rather than letting it
		// fall through to a 404, which would show up as a frontend resource
		// failure in the diagnostics of every single run.
		request.category = "favicon"
		return noContentAssetResponse("image/x-icon", request)
	}
	content, err := fs.ReadFile(provider.assets, assetPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			request.category = "missing"
			return errorAssetResponse(http.StatusNotFound, request)
		}
		request.category = "read_error"
		return errorAssetResponse(http.StatusInternalServerError, request)
	}
	contentType := contentTypeForAsset(assetPath, content)
	return assetResponse{
		status:      http.StatusOK,
		reason:      http.StatusText(http.StatusOK),
		headers:     assetHeaders(contentType),
		contentType: contentType,
		body:        content,
		request:     request,
	}
}

func resolveAssetPath(virtualHost, rawURI string) (string, int) {
	request, status := resolveAssetRequest(virtualHost, rawURI)
	if status != 0 {
		return "", status
	}
	return request.path, status
}

// resolveAssetRequest maps a request URI to an asset path, or to the HTTP status
// that rejects it. The virtual host is passed in rather than read from a package
// constant so that the request filter, the navigation target and this allow-list
// cannot drift apart: all three derive from Config.VirtualHost.
func resolveAssetRequest(virtualHost, rawURI string) (assetRequest, int) {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return assetRequest{path: "invalid", category: "invalid"}, http.StatusBadRequest
	}
	if parsed.Scheme != "https" {
		return assetRequest{path: "wrong_scheme", category: "wrong_scheme"}, http.StatusForbidden
	}
	if parsed.Host != virtualHost {
		return assetRequest{path: "wrong_host", category: "wrong_host"}, http.StatusForbidden
	}
	if containsBackslashOrControl(parsed.Path) || hasTraversalSegment(parsed.Path) {
		return assetRequest{path: "traversal", category: "traversal"}, http.StatusForbidden
	}
	cleanPath := path.Clean("/" + strings.TrimPrefix(parsed.Path, "/"))
	if cleanPath == "/" || cleanPath == "/." {
		cleanPath = "/index.html"
	}
	assetPath := strings.TrimPrefix(cleanPath, "/")
	if assetPath == "" || strings.HasPrefix(assetPath, "../") || assetPath == ".." {
		return assetRequest{path: "traversal", category: "traversal"}, http.StatusForbidden
	}
	return assetRequest{path: assetPath, category: "asset"}, 0
}

func hasTraversalSegment(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

// containsBackslashOrControl rejects input the traversal check above cannot
// reason about. hasTraversalSegment splits on '/' only, and path.Clean (the
// `path` package) treats '\' as an ordinary byte - so a percent-encoded
// backslash (%5c), which url.Parse decodes to a literal '\', survives both as a
// path separator on Windows. A control byte has no place in an asset path
// either. The boundary rejects these itself rather than trusting the caller's
// fs.FS not to resolve them.
func containsBackslashOrControl(value string) bool {
	for index := 0; index < len(value); index++ {
		if char := value[index]; char == '\\' || char < 0x20 || char == 0x7f {
			return true
		}
	}
	return false
}

func noContentAssetResponse(contentType string, request assetRequest) assetResponse {
	return assetResponse{
		status:      http.StatusNoContent,
		reason:      http.StatusText(http.StatusNoContent),
		headers:     assetHeaders(contentType),
		contentType: contentType,
		request:     request,
	}
}

func errorAssetResponse(status int, request assetRequest) assetResponse {
	reason := http.StatusText(status)
	if reason == "" {
		reason = "Error"
	}
	return assetResponse{
		status:      status,
		reason:      reason,
		headers:     assetHeaders("text/plain; charset=utf-8"),
		contentType: "text/plain; charset=utf-8",
		body:        []byte(reason),
		request:     request,
	}
}

func assetHeaders(contentType string) string {
	return "Content-Type: " + contentType + "\r\n" +
		"Cache-Control: no-store, no-cache, must-revalidate, max-age=0\r\n" +
		"Pragma: no-cache\r\n" +
		"Expires: 0\r\n"
}
