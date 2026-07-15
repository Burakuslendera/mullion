//go:build windows

package host

import (
	"github.com/Burakuslendera/mullion/internal/logsafe"
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
			_ = stream.Release()
		}
		return nil, nil, err
	}
	if stream != nil {
		if err := webviewResponse.PutContent(stream); err != nil {
			_ = webviewResponse.Release()
			_ = stream.Release()
			return nil, nil, err
		}
	}
	return webviewResponse, stream, nil
}

// releaseResponse drops this package's references once the response has been
// handed to the runtime.
//
// PutContent takes a reference on the stream and PutResponse takes one on the
// response, so after both have run the runtime owns everything it needs and our
// references are redundant. Holding them until shutdown - which is the easy
// thing to do, and what an earlier version of this code did - makes memory grow
// monotonically with the number of asset requests.
func (provider *assetProvider) releaseResponse(response *webview2.ICoreWebView2WebResourceResponse, stream *webview2.IStream) {
	if response != nil {
		if err := response.Release(); err != nil {
			provider.log.Warn("mullion: asset response release failed, reason=" + logsafe.Reason(err))
		}
	}
	if stream != nil {
		if err := stream.Release(); err != nil {
			provider.log.Warn("mullion: asset stream release failed, reason=" + logsafe.Reason(err))
		}
	}
}
