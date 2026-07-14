//go:build windows

package doctor

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	shcore = windows.NewLazySystemDLL("shcore.dll")

	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procEnumDisplayMonitors           = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfo                = user32.NewProc("GetMonitorInfoW")
	procGetDpiForMonitor              = shcore.NewProc("GetDpiForMonitor")
)

const (
	// dpiAwarenessPerMonitorV2 is DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2,
	// which the API defines as the handle-sized value -4.
	dpiAwarenessPerMonitorV2 = ^uintptr(3)

	mdtEffectiveDPI    = 0
	monitorInfoPrimary = 0x1
	defaultDPI         = 96

	// Windows 11 is a build number, not a name. The registry's ProductName
	// still says "Windows 10" on every Windows 11 machine.
	firstWindows11Build = 22000
)

type rect struct{ Left, Top, Right, Bottom int32 }

// monitorInfoEx mirrors MONITORINFOEXW.
type monitorInfoEx struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
	Device  [32]uint16
}

// Probe gathers the report on this machine.
func Probe(version string) Report {
	// Declared before anything is measured. Windows hands a virtualised
	// resolution to a process that has not asked for per-monitor awareness, so
	// an unaware probe reports "1536x864" for a 1920x1080 monitor at 125% - the
	// one number a DPI bug report must not contain, and the reason this is a
	// tool rather than a checklist.
	_, _, _ = procSetProcessDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2)

	return Report{
		Mullion:  version,
		OS:       windowsVersion(),
		Arch:     runtime.GOARCH,
		Go:       runtime.Version(),
		WebView2: describeWebView2(),
		GPUs:     graphicsAdapters(),
		Monitors: displays(),
		Homes:    homeSpellings(),
	}
}

// homeSpellings collects every name the profile directory answers to, for
// redaction only - the directory itself is never printed.
//
// Both spellings are needed. A profile directory whose name contains a space
// also has an 8.3 short name - the first six characters of the user name, then
// a tilde - and a path that reaches the report in that form sails straight past
// a redaction that only knows the long one. That is not hypothetical: it is
// what the first live run of this command printed.
func homeSpellings() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}

	spellings := []string{expandPath(home)}
	if short := convertPath(home, windows.GetShortPathName); short != "" && !strings.EqualFold(short, spellings[0]) {
		spellings = append(spellings, short)
	}
	return spellings
}

// expandPath resolves a path to its long form, so that a folder handed to us in
// its 8.3 spelling is both readable and redactable. A path that does not exist
// cannot be expanded; it is then returned as it came, which is how the reporter
// typed it.
func expandPath(path string) string {
	if expanded := convertPath(path, windows.GetLongPathName); expanded != "" {
		return expanded
	}
	return path
}

func convertPath(path string, convert func(*uint16, *uint16, uint32) (uint32, error)) string {
	if path == "" {
		return ""
	}
	from, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	// The first call asks how much room the answer needs.
	size, err := convert(from, nil, 0)
	if err != nil || size == 0 {
		return ""
	}
	buffer := make([]uint16, size)
	if written, err := convert(from, &buffer[0], size); err != nil || written == 0 {
		return ""
	}
	return windows.UTF16ToString(buffer)
}

// describeWebView2 asks the loader itself, rather than reading a version out of
// the registry. The registry answers "a runtime is installed"; the loader
// answers "this is the runtime that would be loaded, and it does (or does not)
// still export the entry point we call". Only the second is a diagnosis.
func describeWebView2() WebView2Section {
	section := WebView2Section{
		PinnedEnv: expandPath(strings.TrimSpace(os.Getenv(webview2.BrowserExecutableFolderEnv))),
	}

	found, err := webview2.DescribeRuntime()
	section.ExportName = found.ExportName
	if err != nil {
		section.Problem = err.Error()
		return section
	}

	section.Found = true
	section.Version = found.Version
	section.Folder = expandPath(found.Folder)
	section.Source = found.Source
	section.Fixed = found.Fixed
	section.ExportFound = found.ExportFound
	section.ExportProblem = found.ExportProblem
	return section
}

func windowsVersion() string {
	build := int(windows.RtlGetVersion().BuildNumber)

	edition := "Windows"
	display := ""
	revision := 0

	if key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`,
		registry.QUERY_VALUE,
	); err == nil {
		if value, _, err := key.GetStringValue("ProductName"); err == nil && value != "" {
			edition = value
		}
		if value, _, err := key.GetStringValue("DisplayVersion"); err == nil {
			display = strings.TrimSpace(value)
		}
		if value, _, err := key.GetIntegerValue("UBR"); err == nil {
			revision = int(value)
		}
		key.Close()
	}

	// Reporting ProductName verbatim sends the reader to the wrong Windows, and
	// on a snap or caption bug that is a wasted day: the two shells do not
	// behave the same. The build number is the honest signal.
	if build >= firstWindows11Build && strings.HasPrefix(edition, "Windows 10") {
		edition = "Windows 11" + edition[len("Windows 10"):]
	}

	out := edition
	if display != "" {
		out += " " + display
	}
	return out + " (build " + strconv.Itoa(build) + "." + strconv.Itoa(revision) + ")"
}

// displayAdapterClass is the device class every display adapter registers
// under. It is read instead of WMI because the CIM layer is broken on some
// Windows installations, and a diagnostic that throws on the machine of the
// person trying to help you is worse than one that reports a little less.
const displayAdapterClass = `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`

func graphicsAdapters() []string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, displayAdapterClass, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer key.Close()

	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}

	var found []string
	for _, name := range names {
		// This key has subkeys the current user cannot read. Skip what cannot be
		// read and report what can: giving up on the first refusal would lose the
		// adapters that were readable along with the one that was not.
		sub, err := registry.OpenKey(key, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		description, _, err := sub.GetStringValue("DriverDesc")
		if err != nil || description == "" {
			sub.Close()
			continue
		}
		driver, _, err := sub.GetStringValue("DriverVersion")
		sub.Close()
		if err != nil || driver == "" {
			driver = "unknown"
		}

		entry := description + " (driver " + driver + ")"
		if !listed(found, entry) {
			found = append(found, entry)
		}
	}
	return found
}

func listed(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func displays() []Monitor {
	var found []Monitor

	// NewCallback registers a permanent trampoline: a long-lived process that
	// called this in a loop would leak one per call. This command measures once
	// and exits.
	callback := windows.NewCallback(func(monitor, hdc, clip, data uintptr) uintptr {
		var info monitorInfoEx
		info.Size = uint32(unsafe.Sizeof(info))

		ok, _, _ := procGetMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info)))
		if ok == 0 {
			// Keep enumerating. One monitor that cannot be read must not cost the
			// report every monitor after it.
			return 1
		}

		dpiX, dpiY := uint32(defaultDPI), uint32(defaultDPI)
		_, _, _ = procGetDpiForMonitor.Call(
			monitor,
			mdtEffectiveDPI,
			uintptr(unsafe.Pointer(&dpiX)),
			uintptr(unsafe.Pointer(&dpiY)),
		)

		found = append(found, Monitor{
			Width:      int(info.Monitor.Right - info.Monitor.Left),
			Height:     int(info.Monitor.Bottom - info.Monitor.Top),
			Left:       int(info.Monitor.Left),
			Top:        int(info.Monitor.Top),
			WorkWidth:  int(info.Work.Right - info.Work.Left),
			WorkHeight: int(info.Work.Bottom - info.Work.Top),
			DPI:        int(dpiX),
			Primary:    info.Flags&monitorInfoPrimary != 0,
		})
		return 1
	})

	_, _, _ = procEnumDisplayMonitors.Call(0, 0, callback, 0)
	return found
}
