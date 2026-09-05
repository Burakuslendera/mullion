//go:build windows

package webview2

// Locks the GetProcAddress-failure path of loadClient (issue #51): a client
// DLL that does not export the entry point must produce an error, must not be
// cached, and must release the handle returned by the loader effect. The
// production path still uses the real Windows loader functions; this test
// injects their effects so it never probes process-global module state.
//
// The injected path exercises the same production cleanup branch repeatedly,
// imitating the issue #51 trigger where every failed embed retries the load.
import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestLoadClientFreesTheModuleWhenTheExportIsMissing(t *testing.T) {
	path := t.Name() + ".dll"
	const fakeHandle = windows.Handle(1)
	const rounds = 3
	missingExport := errors.New("missing export")

	var loadCalls, procCalls, freeCalls int
	var loadedPath string
	var procHandle windows.Handle
	var procName string
	var freedHandle windows.Handle
	effects := clientLoaderEffects{
		loadLibrary: func(gotPath string) (windows.Handle, error) {
			loadCalls++
			loadedPath = gotPath
			return fakeHandle, nil
		},
		getProcAddress: func(gotHandle windows.Handle, gotName string) (uintptr, error) {
			procCalls++
			procHandle = gotHandle
			procName = gotName
			return 0, missingExport
		},
		freeLibrary: func(gotHandle windows.Handle) error {
			freeCalls++
			freedHandle = gotHandle
			return nil
		},
	}

	for round := 1; round <= rounds; round++ {
		loaded, err := loadClientWithEffects(path, effects)
		if err == nil {
			t.Fatalf("round %d: loadClient(%s) succeeded, want missing-export error", round, path)
		}
		if loaded != nil {
			t.Fatalf("round %d: loadClient returned a client alongside the error: %+v", round, loaded)
		}
		if !strings.Contains(err.Error(), createEnvironmentExport) {
			t.Fatalf("round %d: error does not name the missing export: %v", round, err)
		}
	}

	if loadedPath != path || loadCalls != rounds {
		t.Fatalf("loader calls = %d path=%q, want %d calls for %q", loadCalls, loadedPath, rounds, path)
	}
	if procCalls != rounds || procHandle != fakeHandle || procName != createEnvironmentExport {
		t.Fatalf("proc calls=%d handle=%v name=%q, want %d/%v/%q",
			procCalls, procHandle, procName, rounds, fakeHandle, createEnvironmentExport)
	}
	if freeCalls != rounds || freedHandle != fakeHandle {
		t.Fatalf("free calls=%d handle=%v, want %d/%v", freeCalls, freedHandle, rounds, fakeHandle)
	}

	clientsMu.Lock()
	_, cached := clients[path]
	clientsMu.Unlock()
	if cached {
		t.Fatalf("the failed load of %s was cached; a later loadClient would reuse a freed handle", path)
	}
}
