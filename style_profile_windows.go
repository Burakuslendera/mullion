//go:build windows

package mullion

type nativeFrameProfile string

const (
	nativeFrameProfileBaseline                 = nativeFrameProfile("baseline")
	nativeFrameProfileSysMenu                  = nativeFrameProfile("sysmenu")
	nativeFrameProfileCaptionNCCalc            = nativeFrameProfile("caption_nccalc")
	nativeFrameProfileCaptionSnapDiag          = nativeFrameProfile("caption_snap_diag")
	nativeFrameProfileCaptionSysMenuNCCalc     = nativeFrameProfile("caption_sysmenu_nccalc")
	nativeFrameProfileCaptionSysMenuSuppressed = nativeFrameProfile("caption_sysmenu_suppressed_caption_diag")
	nativeFrameProfileCaptionSysMenuNative     = nativeFrameProfile("caption_sysmenu_native_nccalc")
	nativeFrameProfileCaptionSysMenuMaxHit     = nativeFrameProfile("caption_sysmenu_maxhit_diag")
	nativeFrameProfileCaptionSysMenuZoomMaxHit = nativeFrameProfile("caption_sysmenu_zoom_maxhit_diag")
	nativeFrameProfileDynamicCaptionHoverSnap  = nativeFrameProfile("dynamic_caption_hover_snap_diag")
	nativeFrameProfileNoCaptionDiagnostic      = nativeFrameProfile("no_caption_diag")
	nativeFrameProfileCaptionButtonsDiag       = nativeFrameProfile("caption_buttons_diag")
)

func styleForNativeFrameProfile(profile nativeFrameProfile, style uintptr) uintptr {
	style &^= uintptr(wsCaption | wsSysMenu | wsMinimizeBox | wsMaximizeBox)
	style |= uintptr(wsThickFrame)
	switch profile {
	case nativeFrameProfileBaseline, nativeFrameProfileNoCaptionDiagnostic, nativeFrameProfileDynamicCaptionHoverSnap:
		style |= uintptr(wsMinimizeBox | wsMaximizeBox)
	case nativeFrameProfileSysMenu:
		style |= uintptr(wsSysMenu | wsMinimizeBox | wsMaximizeBox)
	case nativeFrameProfileCaptionNCCalc:
		style |= uintptr(wsCaption | wsMinimizeBox | wsMaximizeBox)
	case nativeFrameProfileCaptionButtonsDiag:
		style |= uintptr(wsCaption)
	case nativeFrameProfileCaptionSnapDiag:
		style |= uintptr(wsCaption | wsMinimizeBox | wsMaximizeBox)
	case nativeFrameProfileCaptionSysMenuNCCalc, nativeFrameProfileCaptionSysMenuSuppressed, nativeFrameProfileCaptionSysMenuNative,
		nativeFrameProfileCaptionSysMenuMaxHit, nativeFrameProfileCaptionSysMenuZoomMaxHit:
		style |= uintptr(wsCaption | wsSysMenu | wsMinimizeBox | wsMaximizeBox)
	default:
		style |= uintptr(wsMinimizeBox | wsMaximizeBox)
	}
	return style
}

func nativeFrameProfileMatchesStyle(profile nativeFrameProfile, style uintptr) bool {
	if style&uintptr(wsThickFrame) == 0 {
		return false
	}
	hasCaption := style&uintptr(wsCaption) != 0
	hasSysMenu := style&uintptr(wsSysMenu) != 0
	hasMinimizeBox := style&uintptr(wsMinimizeBox) != 0
	hasMaximizeBox := style&uintptr(wsMaximizeBox) != 0
	switch profile {
	case nativeFrameProfileBaseline, nativeFrameProfileNoCaptionDiagnostic, nativeFrameProfileDynamicCaptionHoverSnap:
		return !hasCaption && !hasSysMenu && hasMinimizeBox && hasMaximizeBox
	case nativeFrameProfileSysMenu:
		return !hasCaption && hasSysMenu && hasMinimizeBox && hasMaximizeBox
	case nativeFrameProfileCaptionNCCalc:
		return hasCaption && !hasSysMenu && hasMinimizeBox && hasMaximizeBox
	case nativeFrameProfileCaptionButtonsDiag:
		return hasCaption && !hasSysMenu && !hasMinimizeBox && !hasMaximizeBox
	case nativeFrameProfileCaptionSnapDiag:
		return hasCaption && !hasSysMenu && hasMinimizeBox && hasMaximizeBox
	case nativeFrameProfileCaptionSysMenuNCCalc, nativeFrameProfileCaptionSysMenuSuppressed, nativeFrameProfileCaptionSysMenuNative,
		nativeFrameProfileCaptionSysMenuMaxHit, nativeFrameProfileCaptionSysMenuZoomMaxHit:
		return hasCaption && hasSysMenu && hasMinimizeBox && hasMaximizeBox
	default:
		return false
	}
}

func nativeFrameProfileExtendsClientArea(profile nativeFrameProfile) bool {
	return profile != nativeFrameProfileCaptionSysMenuNative
}

func nativeFrameProfileHandlesNCCalcSize(profile nativeFrameProfile, wParam uintptr) bool {
	return wParam != 0 && nativeFrameProfileExtendsClientArea(profile)
}

func nativeFrameProfileUsesCaptionButtonHitTest(profile nativeFrameProfile) bool {
	return profile == nativeFrameProfileCaptionButtonsDiag
}

func nativeFrameProfileUsesMaximizeCaptionButtonHitTest(profile nativeFrameProfile) bool {
	return profile == nativeFrameProfileCaptionNCCalc ||
		profile == nativeFrameProfileCaptionSnapDiag ||
		profile == nativeFrameProfileCaptionSysMenuMaxHit ||
		profile == nativeFrameProfileDynamicCaptionHoverSnap
}

func nativeFrameProfileUsesZoomedMaximizeCaptionButtonHitTest(profile nativeFrameProfile) bool {
	return profile == nativeFrameProfileCaptionSysMenuZoomMaxHit
}
