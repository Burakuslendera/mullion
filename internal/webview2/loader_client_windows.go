//go:build windows

package webview2

// Loading the runtime's client DLL. Split from loader_windows.go, which keeps
// the creation entry points.

import (
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

const (
	// clientDLL is the runtime's own COM server. It exports
	// CreateWebViewEnvironmentWithOptionsInternal, which is the whole reason
	// mullion does not have to ship WebView2Loader.dll: the loader DLL is a
	// convenience wrapper whose real work happens here.
	clientDLL = "EmbeddedBrowserWebView.dll"

	// createEnvironmentExport is documented by Microsoft as:
	//
	//	STDAPI CreateWebViewEnvironmentWithOptionsInternal(
	//	    bool checkRunningInstance,
	//	    int runtimeType,
	//	    PCWSTR userDataFolder,
	//	    IUnknown *environmentOptions,
	//	    ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler *handler)
	//
	// The argument list is a contract with a native ABI: a missing or reordered
	// argument is not an error return, it is a crash inside the browser process.
	createEnvironmentExport = "CreateWebViewEnvironmentWithOptionsInternal"
)

// loadWithAlteredSearchPath makes the DLL's own directory the first place its
// dependencies are looked for. The runtime DLL resolves siblings out of the
// install folder; without this flag they would be searched for next to our
// executable, which is the wrong folder and, worse, a folder an attacker may be
// able to write to.
const loadWithAlteredSearchPath = 0x00000008

type client struct {
	handle        windows.Handle
	createEnviron ComProc
	path          string
}

// The DLL is loaded once and never freed. Unloading it is not supported: it has
// spawned browser processes, registered COM classes and installed hooks that
// outlive any FreeLibrary we could call.
var (
	clientsMu sync.Mutex
	clients   = make(map[string]*client)
)

func loadClient(path string) (*client, error) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if loaded, ok := clients[path]; ok {
		return loaded, nil
	}

	handle, err := windows.LoadLibraryEx(path, 0, loadWithAlteredSearchPath)
	if err != nil {
		return nil, fmt.Errorf("webview2: loading %s: %w", clientDLL, err)
	}
	address, err := windows.GetProcAddress(handle, createEnvironmentExport)
	if err != nil {
		// The runtime is present but does not export the entry point we need.
		// That means a runtime old enough - or repackaged oddly enough - that
		// it cannot be driven without WebView2Loader.dll.
		return nil, fmt.Errorf("webview2: %s does not export %s: %w", clientDLL, createEnvironmentExport, err)
	}
	loaded := &client{handle: handle, createEnviron: ComProc(address), path: path}
	clients[path] = loaded
	return loaded, nil
}
