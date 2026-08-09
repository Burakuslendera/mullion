//go:build windows

package webview2

// ICoreWebView2EnvironmentOptions, the Go-implemented COM object the runtime
// reads its configuration out of. These exercise the riskiest code in the
// package - a vtable Go hands to native code - without a WebView2 runtime, by
// calling the object through its own vtable exactly as the browser would.

import (
	"sync/atomic"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestEnvironmentOptionsVtblLayout(t *testing.T) {
	var v environmentOptionsVtbl
	checkVtbl(t, "environmentOptionsVtbl", unsafe.Sizeof(v), 11, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"GetAdditionalBrowserArguments", unsafe.Offsetof(v.GetAdditionalBrowserArguments), 3},
		{"PutAdditionalBrowserArguments", unsafe.Offsetof(v.PutAdditionalBrowserArguments), 4},
		{"GetLanguage", unsafe.Offsetof(v.GetLanguage), 5},
		{"PutLanguage", unsafe.Offsetof(v.PutLanguage), 6},
		{"GetTargetCompatibleBrowserVersion", unsafe.Offsetof(v.GetTargetCompatibleBrowserVersion), 7},
		{"PutTargetCompatibleBrowserVersion", unsafe.Offsetof(v.PutTargetCompatibleBrowserVersion), 8},
		{"GetAllowSingleSignOnUsingOSPrimaryAccount", unsafe.Offsetof(v.GetAllowSingleSignOnUsingOSPrimaryAccount), 9},
		{"PutAllowSingleSignOnUsingOSPrimaryAccount", unsafe.Offsetof(v.PutAllowSingleSignOnUsingOSPrimaryAccount), 10},
	})
}

func TestEnvironmentOptionsObjectStartsWithItsVtable(t *testing.T) {
	var object environmentOptions
	var server comServer
	if got := unsafe.Offsetof(object.server); got != 0 {
		t.Fatalf("environmentOptions.server offset = %d, want 0", got)
	}
	if got := unsafe.Offsetof(server.vtbl); got != 0 {
		t.Fatalf("comServer.vtbl offset = %d, want 0", got)
	}
}

// The caller passes stack addresses as uintptr values through this helper.
// Marking them as escaping prevents a stack growth inside the wrapper from
// leaving the callback with an untracked pre-growth address.
//
//go:uintptrescapes
func callCOMSlot(vtable unsafe.Pointer, index uintptr, args ...uintptr) uintptr {
	proc := *(*ComProc)(unsafe.Add(vtable, index*slotSize))
	hr, _, _ := proc.Call(args...)
	return hr
}

func TestEnvironmentOptionsAnswersQueryInterface(t *testing.T) {
	before := liveServerCount()
	options := newEnvironmentOptions(Options{})
	unknown := (*IUnknown)(unsafe.Pointer(options))
	server := serverFor(options.this)
	if server == nil {
		t.Fatal("environment options were not registered")
	}
	wantVtable := uintptr(unsafe.Pointer(&environmentOptionsVtable))
	if server.vtbl != wantVtable {
		t.Fatalf("environment options vtable = %#x, want %#x", server.vtbl, wantVtable)
	}
	vtable := unsafe.Pointer(&environmentOptionsVtable)

	for _, iid := range []windows.GUID{IIDIUnknown, iidEnvironmentOptions} {
		pointer, err := unknown.QueryInterface(&iid)
		if err != nil {
			t.Fatalf("QueryInterface(%s): %v", iid.String(), err)
		}
		if uintptr(pointer) != options.this {
			t.Errorf("QueryInterface(%s) returned a different object", iid.String())
		}
		unknown.Release() // the reference QueryInterface just took
	}

	beforeNull := atomic.LoadInt32(&server.refs)
	var out uintptr = 1
	if hr := callCOMSlot(
		vtable,
		0,
		options.this,
		0,
		uintptr(unsafe.Pointer(&out)),
	); hr != ePointer {
		t.Errorf("QueryInterface(null IID) = %#x, want E_POINTER", hr)
	}
	if out != 0 {
		t.Errorf("QueryInterface(null IID) wrote %#x, want null", out)
	}
	if hr := callCOMSlot(
		vtable,
		0,
		options.this,
		uintptr(unsafe.Pointer(&IIDIUnknown)),
		0,
	); hr != ePointer {
		t.Errorf("QueryInterface(null out) = %#x, want E_POINTER", hr)
	}
	if got := atomic.LoadInt32(&server.refs); got != beforeNull {
		t.Errorf("refcount after invalid QueryInterface calls = %d, want %d", got, beforeNull)
	}
	if got := unknown.AddRef(); got != 2 {
		t.Fatalf("AddRef = %d, want 2", got)
	}
	if got := unknown.Release(); got != 1 {
		t.Fatalf("Release = %d, want 1", got)
	}

	// A runtime probes for interfaces it might use. Claiming one we have not
	// implemented would make it call into an empty vtable slot.
	unsupported := windows.GUID{Data1: 0xdeadbeef}
	if _, err := unknown.QueryInterface(&unsupported); err == nil {
		t.Fatal("QueryInterface must refuse an interface the object does not implement")
	}

	options.release()
	if got := liveServerCount(); got != before {
		t.Fatalf("live COM objects = %d, want %d: the handler outlived its last reference", got, before)
	}
}

func TestEnvironmentOptionsDispatchesByNumericalABISlots(t *testing.T) {
	const (
		args     = "--disable-features=ElasticOverscroll"
		language = "en-GB"
		target   = "123.4.5.6"
	)
	options := newEnvironmentOptions(Options{
		AdditionalBrowserArguments:             args,
		Language:                               language,
		TargetCompatibleBrowserVersion:         target,
		AllowSingleSignOnUsingOSPrimaryAccount: true,
	})
	defer options.release()
	server := serverFor(options.this)
	if server == nil {
		t.Fatal("environment options were not registered")
	}
	wantVtable := uintptr(unsafe.Pointer(&environmentOptionsVtable))
	if server.vtbl != wantVtable {
		t.Fatalf("environment options vtable = %#x, want %#x", server.vtbl, wantVtable)
	}
	vtable := unsafe.Pointer(&environmentOptionsVtable)

	for _, tc := range []struct {
		name  string
		index uintptr
		want  string
	}{
		{"get_AdditionalBrowserArguments", 3, args},
		{"get_Language", 5, language},
		{"get_TargetCompatibleBrowserVersion", 7, target},
	} {
		var value uintptr
		if hr := callCOMSlot(vtable, tc.index, options.this, uintptr(unsafe.Pointer(&value))); hr != sOK {
			t.Fatalf("slot %d (%s) = %#x, want S_OK", tc.index, tc.name, hr)
		}
		if value == 0 {
			t.Fatalf("slot %d (%s) returned null", tc.index, tc.name)
		}
		if got := utf16At(value); got != tc.want {
			t.Fatalf("slot %d (%s) = %q, want %q", tc.index, tc.name, got, tc.want)
		}
		freeCoTaskMem(value)
		if hr := callCOMSlot(vtable, tc.index, options.this, 0); hr != ePointer {
			t.Fatalf("slot %d (%s, null out) = %#x, want E_POINTER", tc.index, tc.name, hr)
		}
	}

	var allowed int32
	if hr := callCOMSlot(vtable, 9, options.this, uintptr(unsafe.Pointer(&allowed))); hr != sOK {
		t.Fatalf("slot 9 (get_AllowSingleSignOnUsingOSPrimaryAccount) = %#x, want S_OK", hr)
	}
	// Win32 BOOL is four bytes; writing a one-byte Go bool would leave three
	// bytes of whatever was on the stack for the runtime to read.
	if allowed != 1 {
		t.Fatalf("slot 9 (get_AllowSingleSignOnUsingOSPrimaryAccount) = %d, want 1", allowed)
	}
	if hr := callCOMSlot(vtable, 9, options.this, 0); hr != ePointer {
		t.Fatalf("slot 9 (get_AllowSingleSignOnUsingOSPrimaryAccount, null out) = %#x, want E_POINTER", hr)
	}

	// These setters are intentionally accepting no-ops, but each literal ABI
	// slot must still dispatch to a setter rather than to a getter with an out
	// parameter of a different shape.
	for _, index := range []uintptr{4, 6, 8, 10} {
		if hr := callCOMSlot(vtable, index, options.this, 0); hr != sOK {
			t.Fatalf("setter slot %d = %#x, want S_OK", index, hr)
		}
	}
}

func TestEnvironmentOptionsUnsetStringUsesNull(t *testing.T) {
	options := newEnvironmentOptions(Options{})
	defer options.release()
	server := serverFor(options.this)
	if server == nil {
		t.Fatal("environment options were not registered")
	}
	wantVtable := uintptr(unsafe.Pointer(&environmentOptionsVtable))
	if server.vtbl != wantVtable {
		t.Fatalf("environment options vtable = %#x, want %#x", server.vtbl, wantVtable)
	}
	vtable := unsafe.Pointer(&environmentOptionsVtable)

	value := uintptr(1)
	if hr := callCOMSlot(
		vtable,
		5,
		options.this,
		uintptr(unsafe.Pointer(&value)),
	); hr != sOK {
		t.Fatalf("slot 5 (get_Language) = %#x, want S_OK", hr)
	}
	if value != 0 {
		t.Fatalf("slot 5 (get_Language) = %#x, want null for an unset property", value)
	}
}

// utf16At reads a NUL-terminated UTF-16 string out of memory the caller does
// not own, without converting the address into a Go pointer.
func utf16At(address uintptr) string {
	const limit = 4096
	units := make([]uint16, 0, 64)
	for offset := uintptr(0); offset < limit; offset += 2 {
		var unit uint16
		_, _, _ = procRtlMoveMemory.Call(
			uintptr(unsafe.Pointer(&unit)),
			address+offset,
			unsafe.Sizeof(unit),
		)
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	return windows.UTF16ToString(units)
}

func freeCoTaskMem(address uintptr) {
	if address == 0 {
		return
	}
	_, _, _ = ole32.NewProc("CoTaskMemFree").Call(address)
}
