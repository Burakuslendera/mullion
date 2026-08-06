//go:build windows

package webview2

// COM objects implemented in Go: the shared IUnknown half every Go-side
// callback object (event handlers, completion handlers, environment options)
// stands on. Split from com_windows.go, which keeps the outbound plumbing.

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// comServer is the IUnknown half of a COM object implemented in Go.
//
// Layout contract: it MUST be the first field of the concrete handler struct.
// A COM interface pointer is the address of a word holding the vtable address,
// so the concrete struct's address is only a valid interface pointer if the
// vtable word sits at offset zero.
type comServer struct {
	vtbl uintptr      // first word: address of the vtable. Do not move.
	refs int32        // COM reference count, touched from any thread
	iid  windows.GUID // the one interface, besides IUnknown, we answer QI for
	self any          // the concrete handler, recovered inside callbacks
}

// servers keeps every live Go-implemented COM object reachable.
//
// This map is the object's only GC root once it has been handed to WebView2:
// the runtime stores the pointer in its own memory, which the Go collector does
// not scan. Without an entry here the handler would be collected while the
// runtime still holds it, and the completion callback would land on freed
// memory. The entry is removed when the COM reference count reaches zero, so
// the lifetime is the COM lifetime, not the process lifetime.
var (
	serversMu sync.Mutex
	servers   = make(map[uintptr]*comServer)
)

// windows.NewCallback allocates from a small, fixed-size table and never frees
// an entry. Keep one process-wide set of vtables, but do not build it at package
// initialisation: unsupported Windows architectures must be rejected before any
// callback trampoline exists. ensureCOMVtables itself checks the central
// architecture decision, so even a direct callback-constructor consumer cannot
// bypass discovery's gate.
//
// The factory is a seam for the architecture-tagged gate test. Production never
// replaces it.
var newCOMCallback = windows.NewCallback

var (
	comVtablesOnce sync.Once

	// These values live at stable package addresses for as long as COM can hold
	// them. They are filled together so no constructor can observe a partial
	// shared IUnknown prefix.
	iunknownVtbl             IUnknownVtbl
	eventHandlerVtable       eventHandlerVtbl
	completedVtable          completionVtbl
	environmentOptionsVtable environmentOptionsVtbl
)

func ensureCOMVtables() bool {
	if ValidateArchitecture() != nil {
		return false
	}
	comVtablesOnce.Do(func() {
		iunknownVtbl = IUnknownVtbl{
			QueryInterface: ComProc(newCOMCallback(serverQueryInterface)),
			AddRef:         ComProc(newCOMCallback(serverAddRef)),
			Release:        ComProc(newCOMCallback(serverRelease)),
		}
		eventHandlerVtable = eventHandlerVtbl{
			IUnknownVtbl: iunknownVtbl,
			Invoke:       ComProc(newCOMCallback(eventHandlerInvoke)),
		}
		completedVtable = completionVtbl{
			IUnknownVtbl: iunknownVtbl,
			Invoke:       ComProc(newCOMCallback(invoked)),
		}
		environmentOptionsVtable = environmentOptionsVtbl{
			IUnknownVtbl:                              iunknownVtbl,
			GetAdditionalBrowserArguments:             ComProc(newCOMCallback(optionsGetAdditionalBrowserArguments)),
			PutAdditionalBrowserArguments:             ComProc(newCOMCallback(optionsPutString)),
			GetLanguage:                               ComProc(newCOMCallback(optionsGetLanguage)),
			PutLanguage:                               ComProc(newCOMCallback(optionsPutString)),
			GetTargetCompatibleBrowserVersion:         ComProc(newCOMCallback(optionsGetTargetCompatibleBrowserVersion)),
			PutTargetCompatibleBrowserVersion:         ComProc(newCOMCallback(optionsPutString)),
			GetAllowSingleSignOnUsingOSPrimaryAccount: ComProc(newCOMCallback(optionsGetAllowSingleSignOn)),
			PutAllowSingleSignOnUsingOSPrimaryAccount: ComProc(newCOMCallback(optionsPutBOOL)),
		}
	})
	return true
}

// register publishes the object to COM with an initial reference count of one
// and returns the interface pointer to hand to the runtime.
//
// vtbl must address a package-level variable: Go globals never move, whereas a
// vtable on the heap would still be reachable but is needlessly hard to reason
// about when debugging a crash inside the runtime.
func (s *comServer) register(vtbl uintptr, iid windows.GUID, self any) uintptr {
	s.vtbl = vtbl
	s.iid = iid
	s.self = self
	atomic.StoreInt32(&s.refs, 1)

	this := uintptr(unsafe.Pointer(s))
	serversMu.Lock()
	servers[this] = s
	serversMu.Unlock()
	return this
}

func serverFor(this uintptr) *comServer {
	serversMu.Lock()
	defer serversMu.Unlock()
	return servers[this]
}

func serverQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return ePointer
	}
	server := serverFor(this)
	if server == nil {
		writeAddress(ppv, 0)
		return eNoInterface
	}
	iid, ok := readGUID(riid)
	if !ok {
		writeAddress(ppv, 0)
		return ePointer
	}
	// Only IUnknown and the object's own interface are supported. Answering
	// E_NOINTERFACE for anything else is not a limitation but the contract:
	// WebView2 probes for newer interfaces it might use, and a truthful "no"
	// makes it fall back instead of calling methods we never implemented.
	if iid == IIDIUnknown || iid == server.iid {
		writeAddress(ppv, this)
		atomic.AddInt32(&server.refs, 1)
		return sOK
	}
	writeAddress(ppv, 0)
	return eNoInterface
}

func serverAddRef(this uintptr) uintptr {
	server := serverFor(this)
	if server == nil {
		return 0
	}
	return uintptr(atomic.AddInt32(&server.refs, 1))
}

func serverRelease(this uintptr) uintptr {
	server := serverFor(this)
	if server == nil {
		return 0
	}
	remaining := atomic.AddInt32(&server.refs, -1)
	if remaining <= 0 {
		// Dropping the map entry drops the last GC root: the handler, and the
		// channel it closed over, become collectable. Nothing is freed by hand;
		// there is no C memory to free.
		serversMu.Lock()
		delete(servers, this)
		serversMu.Unlock()
		return 0
	}
	return uintptr(remaining)
}
