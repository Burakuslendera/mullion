//go:build windows

package webview2

// ICoreWebView2EnvironmentOptions, implemented in Go. Split from
// loader_windows.go, which keeps the creation entry points.

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ICoreWebView2EnvironmentOptions. Taken from the WebView2 SDK's WebView2.h
// (MIDL_INTERFACE declaration), which is the authoritative source: an interface
// ID is an identity, and a wrong one means the runtime silently refuses the
// object.
var iidEnvironmentOptions = windows.GUID{
	Data1: 0x2fde08a8, Data2: 0x1e9a, Data3: 0x4766,
	Data4: [8]byte{0x8c, 0x05, 0x95, 0xa9, 0xce, 0xb9, 0xd1, 0xc5},
}

// Options configures environment creation. The zero value is valid and asks for
// the installed runtime with no extra browser arguments.
type Options struct {
	// UserDataFolder is where the browser keeps its profile. Leave empty to let
	// the runtime pick its default (a folder beside the executable), which
	// fails for an executable installed under Program Files.
	UserDataFolder string

	// AdditionalBrowserArguments are Chromium command line switches.
	AdditionalBrowserArguments string

	// Language is a BCP-47 tag for the browser UI. Empty means the system
	// default, which is what the SDK's own options object reports.
	Language string

	// TargetCompatibleBrowserVersion names the browser build the caller was
	// written against. Empty means "the runtime we found", which is what this
	// package wants: the bindings are hand-written and every optional interface
	// is reached through QueryInterface, so the runtime that is installed is by
	// definition the one we are compatible with.
	//
	// It must not end up null. The runtime validates this property and rejects a
	// null with E_INVALIDARG - WebView2Loader.dll always supplies a value, so the
	// official path never discovers this, but we are not going through it.
	// See resolveTargetVersion.
	TargetCompatibleBrowserVersion string

	// AllowSingleSignOnUsingOSPrimaryAccount enables Azure AD SSO. Off by
	// default: it sends the signed-in Windows identity to the web content.
	AllowSingleSignOnUsingOSPrimaryAccount bool

	// Timeout bounds creation. Zero means DefaultTimeout.
	Timeout time.Duration
}

type environmentOptionsVtbl struct {
	IUnknownVtbl
	GetAdditionalBrowserArguments             ComProc
	PutAdditionalBrowserArguments             ComProc
	GetLanguage                               ComProc
	PutLanguage                               ComProc
	GetTargetCompatibleBrowserVersion         ComProc
	PutTargetCompatibleBrowserVersion         ComProc
	GetAllowSingleSignOnUsingOSPrimaryAccount ComProc
	PutAllowSingleSignOnUsingOSPrimaryAccount ComProc
}

type environmentOptions struct {
	server comServer // must stay first: this is the COM object's identity
	this   uintptr
	opts   Options
}

// The vtable is a package-level value built once. Every windows.NewCallback is
// permanent, so one per method for the whole process is the budget.
var environmentOptionsVtable = environmentOptionsVtbl{
	IUnknownVtbl:                              iunknownVtbl,
	GetAdditionalBrowserArguments:             ComProc(windows.NewCallback(optionsGetAdditionalBrowserArguments)),
	PutAdditionalBrowserArguments:             ComProc(windows.NewCallback(optionsPutString)),
	GetLanguage:                               ComProc(windows.NewCallback(optionsGetLanguage)),
	PutLanguage:                               ComProc(windows.NewCallback(optionsPutString)),
	GetTargetCompatibleBrowserVersion:         ComProc(windows.NewCallback(optionsGetTargetCompatibleBrowserVersion)),
	PutTargetCompatibleBrowserVersion:         ComProc(windows.NewCallback(optionsPutString)),
	GetAllowSingleSignOnUsingOSPrimaryAccount: ComProc(windows.NewCallback(optionsGetAllowSingleSignOn)),
	PutAllowSingleSignOnUsingOSPrimaryAccount: ComProc(windows.NewCallback(optionsPutBOOL)),
}

func newEnvironmentOptions(opts Options) *environmentOptions {
	options := &environmentOptions{opts: opts}
	options.this = options.server.register(
		uintptr(unsafe.Pointer(&environmentOptionsVtable)),
		iidEnvironmentOptions,
		options,
	)
	return options
}

func (o *environmentOptions) release() {
	serverRelease(o.this)
}

func optionsFor(this uintptr) *environmentOptions {
	server := serverFor(this)
	if server == nil {
		return nil
	}
	options, _ := server.self.(*environmentOptions)
	return options
}

// optionsGetString answers a string property. A null result with S_OK means
// "unset", which is exactly what the SDK's reference implementation returns for
// a property that was never assigned - so an empty Go string maps to null
// rather than to an empty string, which the runtime would treat as an explicit
// (and meaningless) value.
func optionsGetString(this, out uintptr, pick func(Options) string) uintptr {
	if out == 0 {
		return ePointer
	}
	options := optionsFor(this)
	if options == nil {
		return eFail
	}
	value, err := coTaskMemString(pick(options.opts))
	if err != nil {
		writeAddress(out, 0)
		return eOutOfMemory
	}
	writeAddress(out, value)
	return sOK
}

func optionsGetAdditionalBrowserArguments(this, out uintptr) uintptr {
	return optionsGetString(this, out, func(o Options) string { return o.AdditionalBrowserArguments })
}

func optionsGetLanguage(this, out uintptr) uintptr {
	return optionsGetString(this, out, func(o Options) string { return o.Language })
}

func optionsGetTargetCompatibleBrowserVersion(this, out uintptr) uintptr {
	return optionsGetString(this, out, func(o Options) string { return o.TargetCompatibleBrowserVersion })
}

func optionsGetAllowSingleSignOn(this, out uintptr) uintptr {
	if out == 0 {
		return ePointer
	}
	options := optionsFor(this)
	if options == nil {
		return eFail
	}
	writeBOOL(out, options.opts.AllowSingleSignOnUsingOSPrimaryAccount)
	return sOK
}

// The setters exist to fill their vtable slots. This object never leaves the
// package - it is created from an Options value, handed to the runtime, and
// released - and the runtime only ever reads it. Accepting and ignoring a write
// keeps a hypothetical setter call from failing environment creation, while
// failing it outright (E_NOTIMPL) would turn a harmless call into a dead
// window.
func optionsPutString(this, value uintptr) uintptr { return sOK }
func optionsPutBOOL(this, value uintptr) uintptr   { return sOK }
