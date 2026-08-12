//go:build windows

package host

import (
	"testing"
	"unsafe"
)

func TestWindowProcRegistryPromotesAndResolvesExactIdentity(t *testing.T) {
	registry := newWindowProcRegistry()
	host := New(Config{})
	const hwnd windowHandle = 0x1234

	token := registry.reserve(host)
	if got := registry.promote(hwnd, token); got != host {
		t.Fatalf("promote host = %p, want %p", got, host)
	}
	if got := registry.resolve(hwnd, token); got != host {
		t.Fatalf("resolve host = %p, want %p", got, host)
	}
	if got := registry.resolve(hwnd, token+1); got != nil {
		t.Fatalf("resolve accepted an unknown token: %p", got)
	}
}

func TestWindowProcRegistryRollsBackFailedCreate(t *testing.T) {
	registry := newWindowProcRegistry()
	host := New(Config{})
	token := registry.reserve(host)

	registry.rollback(token, host)
	if got := registry.promote(0x1234, token); got != nil {
		t.Fatalf("failed-create token still promoted host %p", got)
	}
}

func TestCreateWindowProcTokenReadsCreateParams(t *testing.T) {
	params := struct{ createParams uintptr }{createParams: 0x5678}
	token, ok := createWindowProcToken(uintptr(unsafe.Pointer(&params)))
	if !ok || token != params.createParams {
		t.Fatalf("CREATESTRUCT token = (%#x, %t), want (%#x, true)", token, ok, params.createParams)
	}
	if token, ok := createWindowProcToken(0); ok || token != 0 {
		t.Fatalf("nil CREATESTRUCT token = (%#x, %t), want (0, false)", token, ok)
	}
}

func TestWindowProcRegistryEvictsAndRejectsRecycledHandle(t *testing.T) {
	registry := newWindowProcRegistry()
	first := New(Config{})
	second := New(Config{})
	const hwnd windowHandle = 0x1234

	firstToken := registry.reserve(first)
	if registry.promote(hwnd, firstToken) != first {
		t.Fatal("first host was not promoted")
	}
	registry.evict(hwnd, firstToken)
	if got := registry.resolve(hwnd, firstToken); got != nil {
		t.Fatalf("evicted host remains dispatchable: %p", got)
	}

	secondToken := registry.reserve(second)
	if registry.promote(hwnd, secondToken) != second {
		t.Fatal("recycled HWND did not promote its new host")
	}
	if got := registry.resolve(hwnd, firstToken); got != nil {
		t.Fatalf("recycled HWND resolved the prior host: %p", got)
	}
	if got := registry.resolve(hwnd, secondToken); got != second {
		t.Fatalf("recycled HWND resolved %p, want new host %p", got, second)
	}
}

func TestWindowProcRegistryEvictsEveryRegistrationForDestroyedHandle(t *testing.T) {
	registry := newWindowProcRegistry()
	first := New(Config{})
	second := New(Config{})
	const hwnd windowHandle = 0x1234

	firstToken := registry.reserve(first)
	secondToken := registry.reserve(second)
	if registry.promote(hwnd, firstToken) != first || registry.promote(hwnd, secondToken) != second {
		t.Fatal("hosts were not promoted")
	}

	registry.evictWindow(hwnd)
	for _, token := range []uintptr{firstToken, secondToken} {
		if got := registry.resolve(hwnd, token); got != nil {
			t.Fatalf("destroyed HWND retained host %p for token %#x", got, token)
		}
	}
}

func TestSharedWindowProcIsOneProcessWideCallback(t *testing.T) {
	first := sharedWindowProcCallback()
	second := sharedWindowProcCallback()
	if first == 0 {
		t.Fatal("shared window procedure callback is nil")
	}
	if first != second {
		t.Fatalf("shared window callbacks differ: %#x != %#x", first, second)
	}
}

func TestWindowProcRegistrySkipsActiveTokenAfterCounterWrap(t *testing.T) {
	registry := newWindowProcRegistry()
	active := New(Config{})
	next := New(Config{})
	const hwnd windowHandle = 0x1234

	activeToken := registry.reserve(active)
	if registry.promote(hwnd, activeToken) != active {
		t.Fatal("active host was not promoted")
	}
	registry.next = ^uintptr(0)

	if token := registry.reserve(next); token == activeToken {
		t.Fatalf("counter wrap reused active token %#x", token)
	}
	if got := registry.resolve(hwnd, activeToken); got != active {
		t.Fatalf("counter wrap displaced active host: got %p, want %p", got, active)
	}
}
