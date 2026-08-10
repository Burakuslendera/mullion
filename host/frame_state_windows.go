//go:build windows

package host

import "strconv"

// frontendFrameState is the native pointer-routing state shared by the injected
// drag and resize scripts. The generation changes only at the authoritative
// WM_ENTERSIZEMOVE/WM_EXITSIZEMOVE boundary; each document separately orders
// asynchronous snapshots within one generation.
type frontendFrameState struct {
	maximised      bool
	moveSizeActive bool
	generation     uint64
}

func (host *Host) frontendFrameState() frontendFrameState {
	return frontendFrameState{
		maximised:      host.IsMaximised(),
		moveSizeActive: host.moveSizeActive,
		generation:     host.frameStateGeneration,
	}
}

func (state frontendFrameState) json() string {
	return `{"maximised":` + strconv.FormatBool(state.maximised) +
		`,"moveSizeActive":` + strconv.FormatBool(state.moveSizeActive) +
		`,"generation":` + strconv.FormatUint(state.generation, 10) + `}`
}

func (host *Host) setMoveSizeActive(active bool) {
	if host.moveSizeActive == active {
		return
	}
	host.moveSizeActive = active
	host.frameStateGeneration++
	host.log.Debug("mullion: move-size state changed, active=" + strconv.FormatBool(active) +
		", generation=" + strconv.FormatUint(host.frameStateGeneration, 10))
	host.postFrontendFrameState(host.frontendFrameState())
}

func (host *Host) postFrontendFrameState(state frontendFrameState) {
	payload := `{"event":` + strconv.Quote(eventFrameStateChanged) + `,"state":` + state.json() + `}`
	if host.postFrameState != nil {
		host.warnIf("frame state post", host.postFrameState(payload))
		return
	}
	if host.browser != nil {
		host.warnIf("frame state post", host.browser.PostWebMessage(payload))
	}
}
