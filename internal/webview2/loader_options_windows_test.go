//go:build windows

package webview2

// ICoreWebView2EnvironmentOptions, the Go-implemented COM object the runtime
// reads its configuration out of. These exercise the riskiest code in the
// package - a vtable Go hands to native code - without a WebView2 runtime, by
// calling the object through its own vtable exactly as the browser would.

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestEnvironmentOptionsAnswersQueryInterface(t *testing.T) {
	before := liveServerCount()
	options := newEnvironmentOptions(Options{})
	unknown := (*IUnknown)(unsafe.Pointer(options))

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

func TestEnvironmentOptionsReportsItsValuesToTheRuntime(t *testing.T) {
	const args = "--disable-features=ElasticOverscroll --autoplay-policy=no-user-gesture-required"
	options := newEnvironmentOptions(Options{
		AdditionalBrowserArguments:             args,
		AllowSingleSignOnUsingOSPrimaryAccount: true,
	})
	defer options.release()

	// Call through the vtable, the way the browser does.
	var value uintptr
	hr, _, _ := environmentOptionsVtable.GetAdditionalBrowserArguments.Call(
		options.this,
		uintptr(unsafe.Pointer(&value)),
	)
	if err := hres(hr); err != nil {
		t.Fatalf("get_AdditionalBrowserArguments: %v", err)
	}
	if value == 0 {
		t.Fatal("get_AdditionalBrowserArguments returned null for a value that was set")
	}
	if got := utf16At(value); got != args {
		t.Fatalf("get_AdditionalBrowserArguments = %q, want %q", got, args)
	}
	freeCoTaskMem(value)

	// An unset string must be reported as null, not as an empty string: that is
	// what the SDK's own options object does, and the runtime tells them apart.
	value = 1
	hr, _, _ = environmentOptionsVtable.GetLanguage.Call(options.this, uintptr(unsafe.Pointer(&value)))
	if err := hres(hr); err != nil {
		t.Fatalf("get_Language: %v", err)
	}
	if value != 0 {
		t.Fatalf("get_Language = 0x%x, want null for an unset property", value)
	}

	var allowed int32
	hr, _, _ = environmentOptionsVtable.GetAllowSingleSignOnUsingOSPrimaryAccount.Call(
		options.this,
		uintptr(unsafe.Pointer(&allowed)),
	)
	if err := hres(hr); err != nil {
		t.Fatalf("get_AllowSingleSignOnUsingOSPrimaryAccount: %v", err)
	}
	// Win32 BOOL is four bytes; writing a one-byte Go bool would leave three
	// bytes of whatever was on the stack for the runtime to read.
	if allowed != 1 {
		t.Fatalf("get_AllowSingleSignOnUsingOSPrimaryAccount = %d, want 1", allowed)
	}

	// A null out-parameter must be refused, not dereferenced.
	hr, _, _ = environmentOptionsVtable.GetLanguage.Call(options.this, 0)
	if hr != ePointer {
		t.Fatalf("get_Language(null) = 0x%08X, want E_POINTER", uint32(hr))
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
