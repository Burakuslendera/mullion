//go:build windows

package host

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

func contentTypeForAsset(assetPath string, content []byte) string {
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
	}
	if contentType := mime.TypeByExtension(filepath.Ext(assetPath)); contentType != "" {
		return contentType
	}
	return http.DetectContentType(content)
}
