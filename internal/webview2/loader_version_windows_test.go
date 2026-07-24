//go:build windows

package webview2

// Version handling for runtime discovery: the ordering rule, the
// installed-versus-placeholder test, the target version reported back to the
// runtime, VS_FIXEDFILEINFO decoding, and the control-byte strip applied where a
// version crosses in from the registry. All pure functions - no registry is
// read, no file is opened and no WebView2 install has to exist.

import (
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{"equal", "150.0.4078.65", "150.0.4078.65", 0},
		{"patch newer", "150.0.4078.65", "150.0.4078.48", 1},
		{"patch older", "150.0.4078.48", "150.0.4078.65", -1},
		{"major beats every lower component", "150.0.0.0", "149.9.9999.99", 1},
		{"numeric, not lexicographic", "9.0.0.0", "10.0.0.0", -1},
		{"missing components count as zero", "150.0.4078", "150.0.4078.0", 0},
		{"missing components lose to a set one", "150.0.4078", "150.0.4078.1", -1},
		{"channel suffix is not part of the order", "94.0.992.31 dev", "94.0.992.31", 0},
		{"empty is oldest", "", "0.0.0.1", -1},
		{"both empty", "", "", 0},
		{"garbage component sorts as zero", "150.x.4078.65", "150.0.4078.65", 0},
		{"whitespace tolerated", " 150.0.4078.65 ", "150.0.4078.65", 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := CompareVersions(testCase.a, testCase.b); got != testCase.want {
				t.Fatalf("CompareVersions(%q, %q) = %d, want %d", testCase.a, testCase.b, got, testCase.want)
			}
			// Antisymmetry: a wrong sign here would make runtime selection depend
			// on the order the registry happened to be read in.
			if got, want := CompareVersions(testCase.b, testCase.a), -testCase.want; got != want {
				t.Fatalf("CompareVersions(%q, %q) = %d, want %d", testCase.b, testCase.a, got, want)
			}
		})
	}
}

func TestIsInstalledVersion(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"150.0.4078.65", true},
		{"1", true},
		{"0.0.0.1", true},
		// EdgeUpdate leaves this behind after an uninstall: the client is still
		// registered, but nothing is on disk.
		{"0.0.0.0", false},
		{"0", false},
		{"", false},
		{"   ", false},
	}
	for _, testCase := range cases {
		if got := isInstalledVersion(testCase.version); got != testCase.want {
			t.Errorf("isInstalledVersion(%q) = %t, want %t", testCase.version, got, testCase.want)
		}
	}
}

// TestSanitizeVersionStripsControlBytesButKeepsValid locks the boundary defence:
// a poisoned registry pv (unprivileged-writable HKCU) that smuggles terminal
// escape bytes is cleaned before it can reach the startup log or `mullion
// doctor`, while a legitimate version - digits, dots, an optional channel word -
// is preserved exactly.
func TestSanitizeVersionStripsControlBytesButKeepsValid(t *testing.T) {
	got := sanitizeVersion("9999.0.0.0\x1b]0;pwned\x07\x1b[2K")
	for _, r := range got {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("sanitizeVersion kept control byte %#x: %q", r, got)
		}
	}
	if !strings.HasPrefix(got, "9999.0.0.0") {
		t.Fatalf("sanitizeVersion dropped the version digits: %q", got)
	}
	for _, valid := range []string{"150.0.4078.65", "94.0.992.31 dev"} {
		if got := sanitizeVersion(valid); got != valid {
			t.Fatalf("sanitizeVersion(%q) = %q, want it unchanged", valid, got)
		}
	}
}

// TestResolveTargetVersion locks a fact that only the live runtime could teach:
// ICoreWebView2EnvironmentOptions::get_TargetCompatibleBrowserVersion must never
// answer null. CreateWebViewEnvironmentWithOptionsInternal validates it and
// fails the whole creation with E_INVALIDARG if it is missing, and an
// implausible value ("1.0.0.0") fails with ERROR_FILE_NOT_FOUND. Reporting the
// runtime we actually found is the only answer that always holds.
func TestResolveTargetVersion(t *testing.T) {
	if got := resolveTargetVersion("", "150.0.4078.65"); got != "150.0.4078.65" {
		t.Errorf("resolveTargetVersion = %q, want the discovered runtime version", got)
	}
	if got := resolveTargetVersion("142.0.3595.46", "150.0.4078.65"); got != "142.0.3595.46" {
		t.Errorf("resolveTargetVersion = %q, want the caller's explicit choice to win", got)
	}
	// Nothing is known about the runtime: still not null.
	for _, unknown := range []string{"", "0.0.0.0", "   "} {
		got := resolveTargetVersion("", unknown)
		if got == "" {
			t.Fatalf("resolveTargetVersion(%q, %q) = \"\": a null target version is rejected by the runtime with E_INVALIDARG", "", unknown)
		}
		if got != fallbackTargetVersion {
			t.Errorf("resolveTargetVersion(%q, %q) = %q, want the fallback %q", "", unknown, got, fallbackTargetVersion)
		}
	}
	if !isInstalledVersion(fallbackTargetVersion) {
		t.Fatal("the fallback target version must be a plausible browser version; the runtime rejects one it cannot map to a build")
	}
}

func TestParseFixedFileInfo(t *testing.T) {
	info := make([]byte, 52)
	// signature, strucVersion, fileVersionMS, fileVersionLS
	copy(info, []byte{0xBD, 0x04, 0xEF, 0xFE})
	copy(info[8:], []byte{0x00, 0x00, 0x96, 0x00})  // MS: 150.0
	copy(info[12:], []byte{0x41, 0x00, 0xEE, 0x0F}) // LS: 4078.65

	version, err := parseFixedFileInfo(info)
	if err != nil {
		t.Fatalf("parseFixedFileInfo: %v", err)
	}
	if version != "150.0.4078.65" {
		t.Fatalf("version = %q, want 150.0.4078.65", version)
	}

	if _, err := parseFixedFileInfo(info[:8]); err == nil {
		t.Error("a truncated block must be rejected, not read past its end")
	}
	bad := make([]byte, len(info))
	copy(bad, info)
	bad[0] = 0
	if _, err := parseFixedFileInfo(bad); err == nil {
		t.Error("a bad signature must be rejected: it means we are not looking at VS_FIXEDFILEINFO")
	}
}
