//go:build windows

package host

// System menu item state for the custom title bar.
//
// The window's zoomed state itself is always in sync - IsZoomed/WS_MAXIMIZE track
// the toggle and the double-click correctly - but the *item* states of the standard
// system menu are not: DefWindowProc does not refresh them from the window state on
// WM_INITMENU. Probing a restored window found all six items still enabled, and a
// real caption interaction (maximize) writes the maximized item states into the
// menu and never writes them back on restore.
//
// That is invisible on a stock window, because the shell paints its own caption
// menu. Here the WebView2 non-client right-click path shows whatever GetSystemMenu
// happens to hold, so a restored window would present a maximized menu: Restore
// enabled, Move/Size/Maximise greyed.
//
// So the item states are forced from the real window state, as other frameless
// shells do: on WM_INITMENU, which corrects the menu just before it is shown, and
// eagerly on every WM_SIZE, which also covers the double-click, drag and Win+Z
// transitions that DWM drives without routing through us. The active profile
// carries WS_SYSMENU, so this sync is not optional - it is the only thing keeping
// the menu truthful.

import (
	"strconv"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

const (
	scSize  = 0xF000
	scMove  = 0xF010
	scClose = 0xF060

	mfByCommand = 0x0000
	mfGrayed    = 0x0001

	menuItemMissing = 0xFFFFFFFF
)

var (
	procGetSystemMenu  = user32.NewProc("GetSystemMenu")
	procEnableMenuItem = user32.NewProc("EnableMenuItem")
)

// tabTitlebarSystemMenuItemStates derives the target enabled-state of each standard
// system menu item from the real window state (zoomed/iconic plus the style bits).
// Kept pure so the rules are pinned by unit tests instead of by inspecting a live
// menu: restored means Restore greyed and Move enabled, with Size and Maximise
// following the style bits; maximized or iconic is the mirror image - Restore
// enabled, Move/Size/Maximise greyed. The style bits gate the entries because the
// menu must never offer an action the window cannot actually perform.
func tabTitlebarSystemMenuItemStates(zoomed, iconic bool, style uintptr) map[uintptr]bool {
	restored := !zoomed && !iconic
	return map[uintptr]bool{
		scRestore:  zoomed || iconic,
		scMove:     restored,
		scSize:     restored && style&uintptr(wsThickFrame) != 0,
		scMinimize: style&uintptr(wsMinimizeBox) != 0,
		scMaximize: restored && style&uintptr(wsMaximizeBox) != 0,
		scClose:    true,
	}
}

// sysMenuSnapshot holds the last synced window state so the WM_SIZE storm during an
// interactive resize does not re-issue a burst of user32 calls for a state that has
// not changed. Touched only from the UI thread, hence no lock.
type sysMenuSnapshot struct {
	valid  bool
	zoomed bool
	iconic bool
	style  uintptr
}

// syncTabTitlebarSystemMenuState matches the system menu item states to the real
// window state. The window procedure calls it from wm_initmenu, the moment the menu
// is about to be shown, and from wm_size, on every state transition. wm_size fires
// at mouse-move rate during an interactive resize, so it bails out early when
// nothing changed; wm_initmenu always performs the full sync, as the correctness
// backstop for any transition the eager path missed.
func (host *Host) syncTabTitlebarSystemMenuState(source string) {
	hwnd := host.window()
	if hwnd == 0 {
		return
	}
	style, err := windowStyle(hwnd)
	if err != nil {
		host.log.Warn("mullion: tab titlebar sysmenu style read failed, source=" + logsafe.Message(source) + ", reason=" + logsafe.Reason(err))
		return
	}
	zoomed := isZoomed(hwnd)
	iconic := isIconic(hwnd)
	snapshot := sysMenuSnapshot{valid: true, zoomed: zoomed, iconic: iconic, style: style}
	if source == "wm_size" && host.sysMenuLast == snapshot {
		return
	}
	menu, _, _ := procGetSystemMenu.Call(uintptr(hwnd), 0)
	if menu == 0 {
		host.log.Warn("mullion: tab titlebar sysmenu unavailable, source=" + logsafe.Message(source))
		return
	}
	host.sysMenuLast = snapshot
	missing := 0
	for command, enabled := range tabTitlebarSystemMenuItemStates(zoomed, iconic, style) {
		flags := uintptr(mfByCommand)
		if !enabled {
			flags |= uintptr(mfGrayed)
		}
		previous, _, _ := procEnableMenuItem.Call(menu, command, flags)
		if uint32(previous) == menuItemMissing {
			missing++
		}
	}
	if missing > 0 {
		host.log.Warn("mullion: tab titlebar sysmenu items missing, count=" + strconv.Itoa(missing) + ", source=" + logsafe.Message(source))
	}
	// wm_size fires far too often during an interactive resize to log on; the DEBUG
	// line is written only at the moment the menu is actually shown.
	if source == "wm_initmenu" {
		host.log.Debug("mullion: tab titlebar sysmenu state synced, zoomed=" + strconv.FormatBool(zoomed) +
			", iconic=" + strconv.FormatBool(iconic))
	}
}
