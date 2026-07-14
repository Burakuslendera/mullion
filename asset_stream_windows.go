//go:build windows

package mullion

import "github.com/Burakuslendera/mullion/internal/webview2"

// newAssetStream wraps an asset body in a COM stream.
//
// An empty body is represented by a nil stream rather than an empty one: a 204
// or a 304 has no content, and attaching a zero-length stream to one is a
// contradiction the runtime does not need to resolve.
func newAssetStream(content []byte) (*webview2.IStream, error) {
	if len(content) == 0 {
		return nil, nil
	}
	return webview2.NewMemoryStream(content)
}
