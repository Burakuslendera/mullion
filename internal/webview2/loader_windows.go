//go:build windows

package webview2

// The loader: creating a WebView2 environment and controller by driving the
// runtime's own client DLL directly, with no WebView2Loader.dll shipped.
//
// This file holds the creation entry points; the supporting concerns live in
// their own files of the loader_* family:
//
//	loader_discovery_windows.go   where the runtime lives (env pin, registry)
//	loader_version_windows.go     version parsing, ordering, sanitising
//	loader_client_windows.go      loading the client DLL and its export
//	loader_options_windows.go     ICoreWebView2EnvironmentOptions, in Go
//	loader_completion_windows.go  the completion handler COM objects
//	loader_pump_windows.go        the message pump the waits run on

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DefaultTimeout bounds environment and controller creation. WebView2 has to
// start a browser process, which on a cold machine is slow but not unbounded; a
// caller that waits forever would hang the UI thread with no diagnosis.
const DefaultTimeout = 60 * time.Second

// Environment is a live ICoreWebView2Environment.
//
// It deliberately holds nothing but the COM pointer: the interface's methods
// are declared elsewhere (interfaces_windows.go), and duplicating them here
// would mean two vtable layouts to keep in step with each other.
type Environment struct {
	unknown *IUnknown
}

// Unknown exposes the raw ICoreWebView2Environment pointer.
func (e *Environment) Unknown() *IUnknown {
	if e == nil {
		return nil
	}
	return e.unknown
}

// Release drops the reference taken when the environment was created.
func (e *Environment) Release() {
	if e == nil || e.unknown == nil {
		return
	}
	e.unknown.Release()
	e.unknown = nil
}

// CreateEnvironment creates a WebView2 environment for the runtime installed on
// this machine.
//
// The call is synchronous even though the underlying API is not: WebView2
// delivers the result to a completion handler on the calling thread's message
// queue, so this pumps messages until the handler fires. That means it must be
// called on a thread with a message queue and an initialised STA apartment -
// the same thread that will own the window.
func CreateEnvironment(userDataFolder string, additionalBrowserArgs string) (*Environment, error) {
	return CreateEnvironmentWithOptions(Options{
		UserDataFolder:             userDataFolder,
		AdditionalBrowserArguments: additionalBrowserArgs,
	})
}

// CreateEnvironmentWithOptions is CreateEnvironment with the full option set.
func CreateEnvironmentWithOptions(opts Options) (*Environment, error) {
	found, err := findRuntime()
	if err != nil {
		return nil, err
	}
	loaded, err := loadClient(found.ClientDLL)
	if err != nil {
		return nil, err
	}

	// The completion handler arrives as a window message, so the thread that
	// dispatches it must be the thread that made the call. Locking is cheap
	// insurance: the caller (the host's UI thread) is already locked, and
	// LockOSThread nests.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	opts.TargetCompatibleBrowserVersion = resolveTargetVersion(opts.TargetCompatibleBrowserVersion, found.Version)

	options := newEnvironmentOptions(opts)
	defer options.release()

	handler := newCompletedHandler(uintptr(unsafe.Pointer(&environmentCompletedVtable)), iidEnvironmentCompletedHandler)
	// Our reference is held until Invoke has run. Releasing it right after the
	// create call would rely on the runtime having taken its own reference; it
	// does, but a lifetime bug there is a use-after-free inside the browser, and
	// holding on costs one object.
	defer handler.release()

	var userDataFolder *uint16
	if opts.UserDataFolder != "" {
		if userDataFolder, err = windows.UTF16PtrFromString(opts.UserDataFolder); err != nil {
			return nil, fmt.Errorf("webview2: user data folder: %w", err)
		}
	}

	hr, _, _ := loaded.createEnviron.Call(
		1, // checkRunningInstance: join an already-running runtime for this user
		// data folder instead of failing. Two mullion windows in one process, or
		// a second process sharing the profile, are normal.
		uintptr(found.runtimeType()),
		uintptr(unsafe.Pointer(userDataFolder)),
		options.this,
		handler.this,
	)
	if err := hres(hr); err != nil {
		// A synchronous failure means the runtime never scheduled the completion
		// (the async contract), so nothing can arrive late - but that contract is
		// the runtime's to break, and abandoning costs nothing: if an Invoke ever
		// did fire after this return, it would release its reference instead of
		// stranding it in a buffer nobody drains.
		handler.abandon()
		return nil, fmt.Errorf("webview2: %s: %w", createEnvironmentExport, err)
	}

	result, err := waitFor(handler.done, timeoutOf(opts), "the WebView2 environment")
	if err != nil {
		handler.abandon()
		return nil, err
	}
	unknown, err := completionResult(result, "environment")
	if err != nil {
		return nil, err
	}
	return &Environment{unknown: unknown}, nil
}

// environmentVtbl mirrors ICoreWebView2Environment only as far as its first
// method. The remaining methods belong to the interface bindings; this exists
// so that creating a controller - the one thing the loader must be able to do
// to prove it works - does not depend on them.
type environmentVtbl struct {
	IUnknownVtbl
	CreateCoreWebView2Controller ComProc
}

// CreateController creates the ICoreWebView2Controller that hosts the browser
// inside parent, and returns it as a raw interface pointer for the interface
// bindings to wrap. The caller owns a reference and must Release it.
//
// Like CreateEnvironment, this pumps messages until the completion handler
// fires, and must run on the window's own thread.
func (e *Environment) CreateController(parent windows.Handle) (*IUnknown, error) {
	return e.CreateControllerWithTimeout(parent, DefaultTimeout)
}

// CreateControllerWithTimeout is CreateController with an explicit bound.
func (e *Environment) CreateControllerWithTimeout(parent windows.Handle, timeout time.Duration) (*IUnknown, error) {
	if e == nil || e.unknown == nil {
		return nil, errors.New("webview2: environment is not open")
	}
	if parent == 0 {
		return nil, errors.New("webview2: controller needs a parent window")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	handler := newCompletedHandler(uintptr(unsafe.Pointer(&controllerCompletedVtable)), iidControllerCompletedHandler)
	defer handler.release()

	vtbl := (*environmentVtbl)(unsafe.Pointer(e.unknown.Vtbl))
	hr, _, _ := vtbl.CreateCoreWebView2Controller.Call(
		uintptr(unsafe.Pointer(e.unknown)),
		uintptr(parent),
		handler.this,
	)
	if err := hres(hr); err != nil {
		// Same defence as the environment path: a synchronous failure schedules
		// no completion, but abandoning is free and seals the contract-breach case.
		handler.abandon()
		return nil, fmt.Errorf("webview2: CreateCoreWebView2Controller: %w", err)
	}

	result, err := waitFor(handler.done, timeout, "the WebView2 controller")
	if err != nil {
		handler.abandon()
		return nil, err
	}
	return completionResult(result, "controller")
}

func timeoutOf(opts Options) time.Duration {
	if opts.Timeout > 0 {
		return opts.Timeout
	}
	return DefaultTimeout
}
