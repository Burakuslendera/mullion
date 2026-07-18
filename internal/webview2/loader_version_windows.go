//go:build windows

package webview2

// Version handling for runtime discovery: parsing, ordering, sanitising, and
// reading a version out of a PE file when no registry entry describes the
// runtime. Split from loader_windows.go, which keeps the creation entry points.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// isInstalledVersion rejects the placeholder EdgeUpdate leaves behind when a
// client is registered but not installed.
func isInstalledVersion(version string) bool {
	if strings.TrimSpace(version) == "" {
		return false
	}
	for _, part := range versionParts(version) {
		if part != 0 {
			return true
		}
	}
	return false
}

// sanitizeVersion drops control characters from a version string that came from
// an untrusted source (the EdgeUpdate registry "pv", a folder name). The value is
// logged at startup and printed by `mullion doctor`, so a control byte
// (ESC/BEL/OSC) smuggled into it could inject terminal escape sequences into an
// operator's console through a terminal-rendering Logger. A legitimate version -
// digits, dots, spaces and an optional channel word - contains no control
// character, so stripping them cannot corrupt a real value.
func sanitizeVersion(version string) string {
	version = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, version)
	return strings.TrimSpace(version)
}

// CompareVersions orders two WebView2 version strings, returning -1, 0 or 1.
// It follows CompareBrowserVersions: compare dot-separated numbers left to
// right, with missing components treated as zero, so "150.0.4078" sorts before
// "150.0.4078.65".
//
// Browser version strings may carry a channel suffix ("94.0.992.31 dev"). The
// suffix names a channel, not a rank, so it takes no part in the ordering.
func CompareVersions(a, b string) int {
	left, right := versionParts(a), versionParts(b)
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	for i := 0; i < length; i++ {
		var lv, rv int
		if i < len(left) {
			lv = left[i]
		}
		if i < len(right) {
			rv = right[i]
		}
		switch {
		case lv < rv:
			return -1
		case lv > rv:
			return 1
		}
	}
	return 0
}

func versionParts(version string) []int {
	version = strings.TrimSpace(version)
	if index := strings.IndexByte(version, ' '); index >= 0 {
		version = version[:index]
	}
	if version == "" {
		return nil
	}
	fields := strings.Split(version, ".")
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		// A component that is not a number is not ordered: treating it as zero
		// keeps a malformed version strictly older than a well-formed one
		// rather than crashing the discovery.
		value, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || value < 0 {
			value = 0
		}
		parts = append(parts, value)
	}
	return parts
}

// --- file version --------------------------------------------------------

var (
	versionDLL                 = windows.NewLazySystemDLL("version.dll")
	procGetFileVersionInfoSize = versionDLL.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfo     = versionDLL.NewProc("GetFileVersionInfoW")
	procVerQueryValue          = versionDLL.NewProc("VerQueryValueW")
)

// fileVersion reads a PE file's version resource. It is how a pinned
// fixed-version runtime gets a version number, since no registry entry
// describes it.
func fileVersion(path string) (string, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	size, _, callErr := procGetFileVersionInfoSize.Call(uintptr(unsafe.Pointer(name)), 0)
	if size == 0 {
		return "", fmt.Errorf("webview2: GetFileVersionInfoSize(%s): %w", filepath.Base(path), callErr)
	}

	buffer := make([]byte, size)
	ok, _, callErr := procGetFileVersionInfo.Call(
		uintptr(unsafe.Pointer(name)),
		0,
		size,
		uintptr(unsafe.Pointer(&buffer[0])),
	)
	if ok == 0 {
		return "", fmt.Errorf("webview2: GetFileVersionInfo(%s): %w", filepath.Base(path), callErr)
	}

	root, err := windows.UTF16PtrFromString(`\`)
	if err != nil {
		return "", err
	}
	var block uintptr
	var length uint32
	ok, _, callErr = procVerQueryValue.Call(
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&block)),
		uintptr(unsafe.Pointer(&length)),
	)
	if ok == 0 || block == 0 {
		return "", fmt.Errorf("webview2: VerQueryValue(%s): %w", filepath.Base(path), callErr)
	}

	// VerQueryValue returns a pointer *into* the buffer we own. Rather than
	// convert that address back into a Go pointer, turn it into an offset: the
	// bytes are already in Go memory and can simply be resliced.
	base := uintptr(unsafe.Pointer(&buffer[0]))
	if block < base || block+uintptr(length) > base+uintptr(len(buffer)) {
		return "", errors.New("webview2: version block lies outside the buffer")
	}
	offset := block - base
	return parseFixedFileInfo(buffer[offset : offset+uintptr(length)])
}

// parseFixedFileInfo decodes a VS_FIXEDFILEINFO structure. Split out from the
// Win32 plumbing so the decoding is unit-testable.
func parseFixedFileInfo(info []byte) (string, error) {
	// VS_FIXEDFILEINFO: signature, strucVersion, fileVersionMS, fileVersionLS, ...
	const (
		signature   = 0xFEEF04BD
		minimumSize = 16
	)
	if len(info) < minimumSize {
		return "", errors.New("webview2: version info too short")
	}
	if binary.LittleEndian.Uint32(info[0:4]) != signature {
		return "", errors.New("webview2: version info has a bad signature")
	}
	high := binary.LittleEndian.Uint32(info[8:12])
	low := binary.LittleEndian.Uint32(info[12:16])
	return fmt.Sprintf("%d.%d.%d.%d", high>>16, high&0xFFFF, low>>16, low&0xFFFF), nil
}

// fallbackTargetVersion is the browser build these bindings were written
// against - the value the WebView2 SDK 1.0.3595.46 compiles into its own
// options object (CORE_WEBVIEW_TARGET_PRODUCT_VERSION).
//
// It is only reached when the runtime's version cannot be determined at all,
// which means the registry, the DLL's version resource and the folder name all
// failed to say. Something must be sent: a null target is rejected outright.
const fallbackTargetVersion = "142.0.3595.46"

// resolveTargetVersion decides what to report as ICoreWebView2EnvironmentOptions
// ::get_TargetCompatibleBrowserVersion.
//
// Three behaviours of the runtime shape this, all established by testing the
// export directly rather than by reading the docs:
//
//   - A null value is rejected with E_INVALIDARG. Something must always be sent.
//   - An implausible value ("1.0.0.0") is rejected with ERROR_FILE_NOT_FOUND:
//     the runtime maps the version onto a browser build and finds none.
//   - A value *newer* than the installed runtime ("999.0.0.0") is accepted. The
//     compatibility floor lives in WebView2Loader.dll, which this package does
//     not use, so declaring a high target buys no protection at all. Feature
//     detection has to be done with QueryInterface, and is.
//
// Reporting the version of the runtime we are about to load is therefore both
// the truthful answer and the only one that cannot fail.
func resolveTargetVersion(requested, runtimeVersion string) string {
	if requested = strings.TrimSpace(requested); requested != "" {
		return requested
	}
	if isInstalledVersion(runtimeVersion) {
		return runtimeVersion
	}
	return fallbackTargetVersion
}
