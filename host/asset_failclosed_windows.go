//go:build windows

package host

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Burakuslendera/mullion/internal/logsafe"
	"github.com/Burakuslendera/mullion/internal/webview2"
)

// The embedded asset boundary is a no-network promise (decision 0002): a
// request matched by the filter is answered in process or not at all. The
// WebView2 WebResourceRequested contract is fail-open by default - an event
// that ends without put_Response lets the request continue on the normal
// network stack - so every exit from the asset callback must leave a blocking
// response installed or terminate the session (issue #150). This file owns that
// guarantee; webResourceRequested only records where it gave up.

var (
	errAssetRequestUnavailable   = errors.New("request unavailable")
	errAssetEventArgsUnavailable = errors.New("event args unavailable")
	// errAssetEnvironmentUnavailable marks the exit where no environment object
	// was handed to the callback, so no response can be built from it.
	errAssetEnvironmentUnavailable = errors.New("environment unavailable")
	// errAssetBlockingUnavailable is the blocker reason for an exit where no
	// response object can exist at all: no args to answer on, or no environment
	// to build the response from.
	errAssetBlockingUnavailable = errors.New("no response object can be built or installed")
)

// assetCallback carries one WebResourceRequested invocation from entry to its
// fail-closed exit. webResourceRequested defers finish, so every exit - error,
// panic or normal - passes through it exactly once. Its environment is the
// reference webResourceRequested pinned for the callback's own duration
// (issue #161): finish runs before that pin's release, so block still calls a
// live interface.
type assetCallback struct {
	provider    *assetProvider
	args        *webview2.ICoreWebView2WebResourceRequestedEventArgs
	environment *webview2.ICoreWebView2Environment
	stage       string
	cause       error
	installed   bool
}

// failed records where the callback gave up on serving. The stage names the
// boundary point for the later blocking or escalation report; the caller keeps
// its own warn or error line.
func (callback *assetCallback) failed(stage string, cause error) {
	callback.stage = stage
	callback.cause = cause
}

// finish closes the contract. A callback that installed its response is done;
// anything else receives the deterministic blocking response, or - when even
// that cannot be built or installed - escalates to the terminal teardown. A
// recovered panic is treated like any other failure: blocked, then reported.
// Blocking runs first because the event dispatch's own recover turns a panic
// that escapes this deferred finish into an S_OK return with no response -
// the documented fail-open transition - so the report must not run before the
// answer exists.
func (callback *assetCallback) finish() {
	recovered := recover()
	if recovered != nil {
		callback.stage = "panic"
		callback.cause = errors.New(logsafe.Field(fmt.Sprint(recovered)))
	}
	if !callback.installed {
		callback.block()
	}
	if recovered != nil {
		callback.provider.log.Error("mullion: asset callback panicked, reason=" + logsafe.Reason(callback.cause))
	}
}

// block installs the blocking response: a 500 carrying the boundary's standard
// header block. The content is nil on purpose - the answer's body is not the
// point, the network refusal is - so this path needs no stream and cannot fail
// on stream creation. The creator reference is released after the PutResponse
// attempt, mirroring releaseResponse: on success the runtime retains the
// response; on failure this release is the whole cleanup.
func (callback *assetCallback) block() {
	if callback.args == nil || callback.environment == nil {
		callback.provider.escalate(callback.stage, callback.cause, errAssetBlockingUnavailable)
		return
	}
	response := blockingAssetResponse()
	webviewResponse, err := callback.environment.CreateWebResourceResponse(nil, int32(response.status), response.reason, response.headers)
	if err != nil {
		callback.provider.escalate(callback.stage, callback.cause, err)
		return
	}
	defer webviewResponse.Release()
	if err := callback.args.PutResponse(webviewResponse); err != nil {
		callback.provider.escalate(callback.stage, callback.cause, err)
		return
	}
	callback.provider.log.Error("mullion: asset boundary blocked request, stage=" + logsafe.Field(callback.stage) +
		", reason=" + logsafe.Reason(callback.cause) + ", status=" + strconv.Itoa(response.status))
}

// blockingAssetResponse is the deterministic answer for a request mullion could
// not serve in process. It reuses the served-asset header block - nosniff and
// the no-store family - so a blocking answer can neither be sniffed into
// executable content nor cached. Nothing was resolved, so no diagnostics row
// is recorded for it; the log line carries the stage.
func blockingAssetResponse() assetResponse {
	return errorAssetResponse(http.StatusInternalServerError, assetRequest{path: "fail_closed", category: "fail_closed"})
}

// escalate reports a failure the blocking response could not answer and hands
// the outcome to the host's terminal teardown: the token-validated tagged
// destroy command of the browser-process-exit policy (issue #155, decision
// 0052), posted rather than performed inline, because this runs inside the
// runtime's event dispatch. Run reports ErrAssetBoundaryClosed, so a boundary
// that failed closed can never look like a normal close.
func (provider *assetProvider) escalate(stage string, cause, blocker error) {
	provider.log.Error("mullion: asset boundary escalated, stage=" + logsafe.Field(stage) +
		", reason=" + logsafe.Reason(cause) + ", blocking=" + logsafe.Reason(blocker))
	if provider.terminate == nil {
		// Unreachable from production wiring - Run builds the provider with the
		// terminal seam before any browser exists - but a silent nil here would
		// be a fail-open exit, so the absence is reported as the error it is.
		provider.log.Error("mullion: asset boundary terminal unavailable")
		return
	}
	provider.terminate(stage, cause)
}

// requestAssetBoundaryTerminal latches the asset boundary's fail-closed
// terminal outcome once and schedules the window destruction through the same
// tagged command the browser-process-exit policy uses. A browser already
// shutting down is refused: its teardown owns the outcome, so a user close the
// browser has already claimed stays a normal close in Run's report.
func (host *Host) requestAssetBoundaryTerminal(stage string, cause error) {
	if host.assetBoundaryTerminal {
		return
	}
	if host.browser != nil && host.browser.IsShuttingDown() {
		return
	}
	host.assetBoundaryTerminal = true
	host.log.Debug("mullion: asset boundary terminal requested, stage=" + logsafe.Field(stage) +
		", reason=" + logsafe.Reason(cause))
	if hwnd := host.window(); hwnd != 0 {
		host.warnIf("asset boundary terminal post", host.postRunCommand(host.currentRun(), wmNativeProcessExit, 0))
	}
}
