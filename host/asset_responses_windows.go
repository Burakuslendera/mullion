//go:build windows

package host

import (
	"github.com/Burakuslendera/mullion/internal/webview2"
)

// createWebResourceResponse builds the COM response for one asset.
//
// Reference counting here is the whole game. The stream must be created first
// and attached with PutContent, never handed to CreateWebResourceResponse and
// released on the way out: a response whose body stream has already been freed
// serves an empty document, and the symptom is not an error but a blank window
// with zero scripts and zero stylesheets loaded.
func (provider *assetProvider) createWebResourceResponse(environment *webview2.ICoreWebView2Environment, response assetResponse) (*webview2.ICoreWebView2WebResourceResponse, *webview2.IStream, error) {
	stream, err := newAssetStream(response.body)
	if err != nil {
		return nil, nil, err
	}
	webviewResponse, err := environment.CreateWebResourceResponse(nil, int32(response.status), response.reason, response.headers)
	if err != nil {
		if stream != nil {
			stream.Release()
		}
		return nil, nil, err
	}
	if stream != nil {
		if err := webviewResponse.PutContent(stream); err != nil {
			webviewResponse.Release()
			stream.Release()
			return nil, nil, err
		}
	}
	return webviewResponse, stream, nil
}

// releaseResponse drops the creator references after the PutResponse attempt.
//
// A successful PutContent makes the response retain the stream. A successful
// PutResponse then makes the runtime retain the response, so dropping both
// creator references leaves the runtime's response-to-stream chain alive. If
// PutResponse fails, the runtime owns no response reference: releasing the
// creator response drops its PutContent stream reference, and releasing the
// creator stream completes cleanup. Retaining creator references until shutdown
// would make memory grow with the number of asset requests.
func (provider *assetProvider) releaseResponse(response *webview2.ICoreWebView2WebResourceResponse, stream *webview2.IStream) {
	if response != nil {
		response.Release()
	}
	if stream != nil {
		stream.Release()
	}
}
