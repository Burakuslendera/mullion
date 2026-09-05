//go:build windows

package webview2

// These tests inspect the installed WebView2 runtime and its client export on
// the host machine. They are opt-in: without MULLION_REQUIRE_WEBVIEW2=1 each
// test skips before any discovery or DLL access, so the default suite remains
// usable without a runtime. The explicit machine lane sets the variable; an
// absent required runtime or export then fails instead of skipping.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requireWebView2Machine is the first operation in each machine test below.
// Keeping the gate before FindRuntime, RuntimeClientPath, and DescribeRuntime
// makes an ordinary test run free of registry, filesystem, and DLL probing.
const requireWebView2Env = "MULLION_REQUIRE_WEBVIEW2"

type machineGateT interface {
	Helper()
	Skipf(format string, args ...any)
}

func requireWebView2Machine(t machineGateT) {
	t.Helper()
	if os.Getenv(requireWebView2Env) != "1" {
		t.Skipf("%s=1 is required for machine WebView2 checks", requireWebView2Env)
	}
}

type machineGateRecordingT struct {
	skipped bool
}

func (*machineGateRecordingT) Helper() {}
func (r *machineGateRecordingT) Skipf(string, ...any) {
	r.skipped = true
}

func TestRequireWebView2MachineGate(t *testing.T) {
	t.Setenv(requireWebView2Env, "")
	relaxed := &machineGateRecordingT{}
	requireWebView2Machine(relaxed)
	if !relaxed.skipped {
		t.Fatal("unset MULLION_REQUIRE_WEBVIEW2 must skip machine checks")
	}

	t.Setenv(requireWebView2Env, "1")
	strict := &machineGateRecordingT{}
	requireWebView2Machine(strict)
	if strict.skipped {
		t.Fatal("MULLION_REQUIRE_WEBVIEW2=1 must run machine checks")
	}
}

func TestFindRuntimeOnThisMachine(t *testing.T) {
	requireWebView2Machine(t)

	folder, version, err := FindRuntime()
	if err != nil {
		t.Fatalf("%s=1 but no WebView2 runtime is available: %v", requireWebView2Env, err)
	}
	if folder == "" || version == "" {
		t.Fatalf("FindRuntime returned folder=%q version=%q; both must be set for an Evergreen install", folder, version)
	}
	client, err := RuntimeClientPath()
	if err != nil {
		t.Fatalf("%s=1 but RuntimeClientPath found no WebView2 runtime: %v", requireWebView2Env, err)
	}
	if _, err := os.Stat(client); err != nil {
		t.Fatalf("FindRuntime chose %q, which is not on disk: %v", client, err)
	}
	if !strings.EqualFold(filepath.Base(client), clientDLL) {
		t.Fatalf("client = %q, want %s", client, clientDLL)
	}

	// The registry's version and the binary's own version describe the same
	// install; if they disagree, discovery picked a folder from one install and
	// a version from another.
	binary, err := fileVersion(client)
	if err != nil {
		t.Fatalf("cannot read the version resource of %s at %q: %v", clientDLL, client, err)
	}
	if CompareVersions(binary, version) != 0 {
		t.Fatalf("registry reports %q but %s reports %q", version, clientDLL, binary)
	}
}

func TestRuntimeExportsTheEntryPointWeCallDirectly(t *testing.T) {
	requireWebView2Machine(t)

	path, err := RuntimeClientPath()
	if err != nil {
		t.Fatalf("%s=1 but no WebView2 runtime is available: %v", requireWebView2Env, err)
	}
	// Loading the DLL starts no browser process; it only proves that the export
	// this package is built on is really there. If Microsoft ever removes it,
	// this is the test that says so.
	loaded, err := loadClient(path)
	if err != nil {
		t.Fatalf("loadClient(%s): %v", clientDLL, err)
	}
	if loaded.createEnviron == 0 {
		t.Fatalf("%s exports no %s", clientDLL, createEnvironmentExport)
	}
}
