//go:build windows

package mullion

import (
	"strconv"
)

type nativeCaptionRoute string

const (
	nativeCaptionRouteProject       = nativeCaptionRoute("project")
	nativeCaptionRouteDWM           = nativeCaptionRoute("dwm")
	nativeCaptionRouteDefWindowProc = nativeCaptionRoute("defwindowproc")
)

type nativeCaptionDecision struct {
	result     uintptr
	route      nativeCaptionRoute
	dwmResult  uintptr
	dwmHandled bool
	useDWM     bool
	override   bool
}

func (host *Host) nativeDWMCaptionHitTestDecision(hwnd windowHandle, message uint32, wParam, lParam uintptr, projectHit, candidateHit uintptr) nativeCaptionDecision {
	decision := nativeCaptionDecision{result: projectHit, route: nativeCaptionRouteProject}
	policy := activeDWMCaptionPolicyForWindow(hwnd)
	if policy == nativeDWMCaptionPolicyDisabled {
		return decision
	}
	dwmHit, handled := dwmDefWindowProc(hwnd, message, wParam, lParam)
	decision.dwmResult = dwmHit
	decision.dwmHandled = handled
	useDWM := shouldUseDWMCaptionHitForPolicy(dwmHit, handled, policy)
	decision.useDWM = useDWM
	host.log.Debug(formatDWMCaptionDiagnosticLog("hittest", message, projectHit, dwmHit, handled, useDWM))
	if useDWM {
		decision.result = dwmHit
		decision.route = nativeCaptionRouteDWM
		decision.override = true
		return decision
	}
	if shouldUseCaptionPassthroughForPolicy(int32(candidateHit), policy) {
		decision.result = defWindowProc(hwnd, message, wParam, lParam)
		decision.route = nativeCaptionRouteDefWindowProc
		decision.override = true
	}
	return decision
}

func (host *Host) nativeDWMCaptionMessageDecision(hwnd windowHandle, message uint32, wParam, lParam uintptr) nativeCaptionDecision {
	decision := nativeCaptionDecision{route: nativeCaptionRouteDefWindowProc}
	policy := activeDWMCaptionPolicyForWindow(hwnd)
	if policy == nativeDWMCaptionPolicyDisabled {
		return decision
	}
	hit := nativeCaptionMessageHit(message, wParam, lParam)
	dwmResult, handled := dwmDefWindowProc(hwnd, message, wParam, lParam)
	decision.dwmResult = dwmResult
	decision.dwmHandled = handled
	useDWM := handled && shouldUseDWMCaptionMessageHitForPolicy(hit, policy)
	decision.useDWM = useDWM
	host.log.Debug(formatDWMCaptionDiagnosticLog("message", message, uintptr(hit), dwmResult, handled, useDWM))
	if useDWM {
		decision.result = dwmResult
		decision.route = nativeCaptionRouteDWM
		decision.override = true
		return decision
	}
	if shouldUseCaptionPassthroughForPolicy(hit, policy) {
		decision.result = defWindowProc(hwnd, message, wParam, lParam)
		decision.route = nativeCaptionRouteDefWindowProc
		decision.override = true
	}
	return decision
}

func shouldUseDWMCaptionHit(dwmHit uintptr, handled bool) bool {
	return shouldUseDWMCaptionHitForPolicy(dwmHit, handled, nativeDWMCaptionPolicyAllButtons)
}

type nativeDWMCaptionPolicy int

const (
	nativeDWMCaptionPolicyDisabled nativeDWMCaptionPolicy = iota
	nativeDWMCaptionPolicyMaximizeOnly
	nativeDWMCaptionPolicyAllButtons
)

func activeDWMCaptionPolicy() nativeDWMCaptionPolicy {
	if nativeDWMCaptionDiagnosticEnabled() {
		return nativeDWMCaptionPolicyAllButtons
	}
	if nativeFrameProfileUsesDWMMaximizeCaptionButton(activeNativeFrameProfile()) {
		return nativeDWMCaptionPolicyMaximizeOnly
	}
	return nativeDWMCaptionPolicyDisabled
}

func activeDWMCaptionPolicyForWindow(hwnd windowHandle) nativeDWMCaptionPolicy {
	policy := activeDWMCaptionPolicy()
	if policy != nativeDWMCaptionPolicyDisabled {
		return policy
	}
	if nativeFrameProfileUsesZoomedDWMMaximizeCaptionButton(activeNativeFrameProfile()) && isZoomed(hwnd) {
		return nativeDWMCaptionPolicyMaximizeOnly
	}
	return policy
}

func nativeFrameProfileUsesDWMMaximizeCaptionButton(profile nativeFrameProfile) bool {
	switch profile {
	case nativeFrameProfileCaptionNCCalc, nativeFrameProfileCaptionSnapDiag, nativeFrameProfileCaptionSysMenuMaxHit,
		nativeFrameProfileDynamicCaptionHoverSnap:
		return true
	default:
		return false
	}
}

func nativeFrameProfileUsesZoomedDWMMaximizeCaptionButton(profile nativeFrameProfile) bool {
	return profile == nativeFrameProfileCaptionSysMenuZoomMaxHit
}

func nativeFrameProfileUsesDynamicSnapCaption(profile nativeFrameProfile) bool {
	return profile == nativeFrameProfileDynamicCaptionHoverSnap
}

func shouldUseDWMCaptionHitForPolicy(dwmHit uintptr, handled bool, policy nativeDWMCaptionPolicy) bool {
	if !handled {
		return false
	}
	return shouldUseDWMCaptionMessageHitForPolicy(int32(dwmHit), policy)
}

func shouldUseDWMCaptionMessageHitForPolicy(hit int32, policy nativeDWMCaptionPolicy) bool {
	switch policy {
	case nativeDWMCaptionPolicyAllButtons:
		return isNativeCaptionButtonHit(hit)
	case nativeDWMCaptionPolicyMaximizeOnly:
		return hit == htMaxButton
	default:
		return false
	}
}

func shouldUseCaptionPassthroughForPolicy(hit int32, policy nativeDWMCaptionPolicy) bool {
	return nativeCaptionPassthroughDiagnosticEnabled() &&
		policy == nativeDWMCaptionPolicyMaximizeOnly &&
		hit == htMaxButton
}

func nativeCaptionMessageHit(message uint32, wParam, lParam uintptr) int32 {
	if message == wmSetCursor {
		return int32(lParam & 0xffff)
	}
	return int32(wParam)
}

func isNativeCaptionButtonHit(hit int32) bool {
	switch hit {
	case htMinButton, htMaxButton, htClose:
		return true
	default:
		return false
	}
}

func formatDWMCaptionDiagnosticLog(source string, message uint32, projectOrMessageHit, dwmResult uintptr, handled bool, useDWM bool) string {
	return "mullion: dwm caption diagnostic, source=" + source +
		", message=" + formatNativeTooltipMessage(message) +
		", project_or_message_hit=" + formatNativeTooltipHit(int32(projectOrMessageHit)) +
		", dwm_result=" + formatNativeTooltipHit(int32(dwmResult)) +
		", dwm_handled=" + strconv.FormatBool(handled) +
		", use_dwm=" + strconv.FormatBool(useDWM)
}

func formatNativeCaptionRoute(route nativeCaptionRoute) string {
	return string(route)
}
