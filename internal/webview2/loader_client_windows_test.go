//go:build windows

package webview2

// Locks the GetProcAddress-failure path of loadClient (issue #51): a client
// DLL that is on disk but does not export the entry point must produce an
// error, must not be cached, and must not stay resident. The FreeLibrary on
// that path balances the LoadLibraryEx reference, so repeated embed retries
// (a failed embed leaves nothing cached, and every later Show reloads) no
// longer accumulate module references.
//
// The branch condition is "a DLL exists at the path and lacks the export", so
// any system DLL reproduces it faithfully: loadClient only maps the file and
// looks up one symbol. No fake runtime has to be built, no window is created
// and no browser process is started - headless by construction.

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// missingExportDLL picks a system DLL that this process has not loaded, so
// whether the module is resident afterwards is entirely loadClient's doing.
// The candidates ship with every supported Windows, and neither the Go
// runtime nor this test binary loads them on its own; if one is resident
// anyway, the next candidate is tried.
func missingExportDLL(t *testing.T) (path, base string) {
	t.Helper()
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatalf("GetSystemDirectory: %v", err)
	}
	var rejected []string
	for _, name := range []string{"msvfw32.dll", "imagehlp.dll", "cabinet.dll"} {
		if moduleIsLoaded(t, name) {
			// Already resident: the refcount observation below would be blind,
			// because another holder would keep the module alive across a
			// balanced load/free pair.
			rejected = append(rejected, name+" (already loaded)")
			continue
		}
		candidate := filepath.Join(systemDir, name)
		if !fileExists(candidate) {
			rejected = append(rejected, name+" (not on disk)")
			continue
		}
		return candidate, name
	}
	t.Skipf("no usable candidate DLL: %s", strings.Join(rejected, ", "))
	return "", ""
}

func moduleIsLoaded(t *testing.T, base string) bool {
	t.Helper()
	name, err := windows.UTF16PtrFromString(base)
	if err != nil {
		t.Fatalf("UTF16PtrFromString(%s): %v", base, err)
	}
	// UNCHANGED_REFCOUNT matters: without it GetModuleHandleEx takes a
	// reference of its own, and the probe would keep alive the very module
	// whose release it is supposed to observe.
	var module windows.Handle
	err = windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, name, &module)
	return err == nil && module != 0
}

func TestLoadClientFreesTheModuleWhenTheExportIsMissing(t *testing.T) {
	path, base := missingExportDLL(t)

	// Three rounds imitate the issue #51 trigger, where every failed embed
	// retries the load from scratch. Each round must leave the ledger where it
	// found it; before the fix, each one pinned an extra module reference.
	for round := 1; round <= 3; round++ {
		loaded, err := loadClient(path)
		if err == nil {
			t.Fatalf("round %d: loadClient(%s) succeeded, but %s should not export %s", round, base, base, createEnvironmentExport)
		}
		if loaded != nil {
			t.Fatalf("round %d: loadClient returned a client alongside the error: %+v", round, loaded)
		}
		if !strings.Contains(err.Error(), createEnvironmentExport) {
			t.Fatalf("round %d: the error does not name the missing export: %v", round, err)
		}
		if moduleIsLoaded(t, base) {
			t.Fatalf("round %d: %s is still resident after the failed load; the LoadLibraryEx reference was not released", round, base)
		}
	}

	// A failed load must not be cached either: a cached entry would hand a
	// freed handle to the next caller instead of retrying the load.
	clientsMu.Lock()
	_, cached := clients[path]
	clientsMu.Unlock()
	if cached {
		t.Fatalf("the failed load of %s was cached; a later loadClient would reuse a freed handle", base)
	}
}
