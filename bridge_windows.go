//go:build windows

package mullion

import (
	"encoding/json"
	"strconv"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

// bridgeRequest is the wire format the injected bridge posts. Only the fields
// the host needs are decoded; an application-defined call is forwarded to
// Config.Bridge as the original raw string, so the application's own protocol
// stays opaque to the library.
type bridgeRequest struct {
	ID     string            `json:"id"`
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

// handleWebMessage routes one message from the frontend. It returns the reply to
// post back, or "" when there is nothing to say.
//
// Window control methods are answered here rather than being handed to
// Config.Bridge. That is what makes the title bar work out of the box: an
// application that only wants a window - no application methods at all - can
// leave Config.Bridge nil and still get drag, resize, minimise, maximise and
// close.
func (host *Host) handleWebMessage(raw string) string {
	var request bridgeRequest
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		// A frontend can post arbitrary strings through chrome.webview; a
		// malformed one is a frontend bug, not a reason to tear down the window.
		host.log.Warn("mullion: bridge message unparsable, reason=" + logsafe.Reason(err))
		return ""
	}
	if request.Method == "" {
		host.log.Warn("mullion: bridge message without method")
		return ""
	}

	if reply, handled := host.handleReservedMethod(request); handled {
		return reply
	}

	if host.config.Bridge == nil {
		host.log.Warn("mullion: bridge method unhandled, no bridge configured, method=" + logsafe.Message(request.Method))
		return bridgeError(request.ID, "no bridge configured")
	}
	host.recordBridgeCall(request.Method, "received")
	response := host.config.Bridge(raw)
	host.recordBridgeCall(request.Method, "completed")
	return response
}

func (host *Host) handleReservedMethod(request bridgeRequest) (string, bool) {
	switch request.Method {
	case methodStartDrag:
		host.StartDrag()
	case methodStartResize:
		host.StartResize(stringArg(request.Args, 0))
	case methodMinimise:
		host.Minimise()
	case methodToggleMaximise:
		host.ToggleMaximise()
	case methodIsMaximised:
		return bridgeResult(request.ID, strconv.FormatBool(host.IsMaximised())), true
	case methodShow:
		if err := host.Show(); err != nil {
			return bridgeError(request.ID, "show failed"), true
		}
	case methodHide:
		host.Hide()
	case methodClose:
		host.Quit()
	case methodShellReady:
		host.MarkFrontendShellReady()
	case methodReady:
		host.MarkFrontendReady()
	case methodPhase:
		host.MarkFrontendPhase(stringArg(request.Args, 0))
	case methodDiagnostic:
		host.MarkFrontendDiagnostic(stringArg(request.Args, 0), stringArg(request.Args, 1))
	default:
		return "", false
	}
	return bridgeAck(request.ID), true
}

// stringArg reads one positional argument. A frontend may send a non-string
// where a string is expected; that yields "" rather than an error, because the
// callers all validate their own input anyway (an unknown resize edge is
// rejected and logged by StartResize).
func stringArg(args []json.RawMessage, index int) string {
	if index >= len(args) {
		return ""
	}
	var value string
	if err := json.Unmarshal(args[index], &value); err != nil {
		return ""
	}
	return value
}

// bridgeAck answers a fire-and-forget call. The injected bridge does not await
// these, but replying keeps the protocol uniform and lets a custom frontend
// await one if it wants to.
func bridgeAck(id string) string {
	if id == "" {
		return ""
	}
	return `{"id":` + strconv.Quote(id) + `,"ok":true}`
}

func bridgeResult(id string, rawResult string) string {
	if id == "" {
		return ""
	}
	return `{"id":` + strconv.Quote(id) + `,"ok":true,"result":` + rawResult + `}`
}

func bridgeError(id string, reason string) string {
	if id == "" {
		return ""
	}
	return `{"id":` + strconv.Quote(id) + `,"ok":false,"error":` + strconv.Quote(reason) + `}`
}

// recordBridgeCall feeds the diagnostics that the render watchdog reports when
// the frontend never signals readiness.
func (host *Host) recordBridgeCall(method string, status string) {
	host.diagnostics.recordBridge(method, status)
	host.log.Debug("mullion: bridge method " + logsafe.Message(status) + ", method=" + logsafe.Message(method))
}
