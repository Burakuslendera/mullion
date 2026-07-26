//go:build windows

package host

import (
	"strings"
	"testing"
)

// The system-browser launch leaves the UI thread, and the bound that keeps it
// from turning into a goroutine pump (issue #74, decisions/0029). Where a URL
// gets routed is next door in systembrowser_windows_test.go; this file is only
// about how the launch runs once the routing has decided to make one.

// The bound, driven directly. openInSystemBrowser cannot drive it: the test seam
// returns ahead of the claim, deliberately, so that a routing test asserts on a
// decision rather than on a goroutine finishing. That leaves the bound on a path
// no other test reaches, which is why it is exercised here at the seam below it.
func TestExternalOpenSlotsAreBoundedAndSayWhenTheyRunOut(t *testing.T) {
	host, logger := newTestHost(t, Config{})

	for i := 0; i < externalOpenLimit; i++ {
		if !host.claimExternalOpenSlot("https://example.test/a") {
			t.Fatalf("launch %d of %d was refused while the bound still had room", i+1, externalOpenLimit)
		}
	}
	if strings.Contains(logger.String(), "external open dropped") {
		t.Fatalf("a launch was dropped before the bound was reached:\n%s", logger.String())
	}

	if host.claimExternalOpenSlot("https://evil.example/spam") {
		t.Fatal("the bound admitted one launch too many: unbounded, this is a goroutine and an OS thread per window.open, with the page choosing how many")
	}
	logged := logger.String()
	if !strings.Contains(logged, "external open dropped") {
		t.Fatalf("the dropped launch was not reported - a click of the user's goes nowhere and nothing says so:\n%s", logged)
	}
	if !strings.Contains(logged, "https://evil.example/spam") {
		t.Fatalf("the dropped launch does not name its target:\n%s", logged)
	}

	// A finished launch frees its slot. Without this the bound saturates
	// permanently after externalOpenLimit launches and every later click is lost.
	host.releaseExternalOpenSlot()
	if !host.claimExternalOpenSlot("https://example.test/b") {
		t.Fatal("a released slot was not reusable")
	}
}

// A host that never reached Run still routes, so the slots have to exist by the
// time an event handler can reach them. A nil channel is not empty - it blocks
// forever on send, so the select below it takes the default branch - which would
// turn "bounded" into "every launch dropped", silently, with a warning per click.
func TestExternalOpenSlotsExistOnAFreshHost(t *testing.T) {
	host := New(Config{})
	if host.externalOpenSlots == nil {
		t.Fatal("New left the launch slots nil: every launch would be dropped as though the bound were full")
	}
	if got := cap(host.externalOpenSlots); got != externalOpenLimit {
		t.Fatalf("launch slots hold %d, want externalOpenLimit (%d)", got, externalOpenLimit)
	}
}

// The launch must not run on the calling thread, and nothing in this suite can
// see that by running it: the seam that makes routing testable returns before the
// goroutine exists, and the goroutine's own work is the ShellExecute a headless
// suite must never reach (issue #76). So the ordering is guarded by reading the
// source, the way the navigation callbacks' is in
// navigation_report_source_test.go.
func TestTheSystemBrowserLaunchLeavesTheUIThread(t *testing.T) {
	source := sourceComment.ReplaceAllString(readRepoFile(t, "host", "systembrowser_windows.go"), "")

	body := externalOpenSource(t, source, "func (host *Host) openInSystemBrowser(", "func (host *Host) claimExternalOpenSlot(")
	if strings.Contains(body, "procShellExecute") {
		t.Fatal("openInSystemBrowser calls ShellExecute itself: it runs from a WebView2 event handler on the UI thread, so the message loop stops pumping until the browser has started and the window stops answering (issue #74)")
	}
	launch := strings.Index(body, "go func()")
	if launch < 0 {
		t.Fatal("openInSystemBrowser no longer hands the launch to a goroutine")
	}
	call := strings.Index(body, "host.shellExecuteOpen(")
	if call < 0 {
		t.Fatal("openInSystemBrowser no longer performs the launch at all")
	}
	if call < launch {
		t.Fatal("shellExecuteOpen runs on the calling thread rather than from the goroutine: the goroutine below it changes nothing (issue #74)")
	}

	// And the slot the claim took has to be given back, on the goroutine. The
	// bound case above calls releaseExternalOpenSlot directly - it has to, the
	// seam returns before the goroutine exists - so it covers the function and
	// not this call site. Deleting the release here left the whole suite green
	// while the bound saturated permanently after externalOpenLimit launches,
	// dropping every later click with a warning: the failure the bound case
	// describes in a comment and cannot reach.
	release := strings.Index(body, "host.releaseExternalOpenSlot()")
	if release < 0 {
		t.Fatal("openInSystemBrowser never gives the launch slot back: after externalOpenLimit launches the bound is saturated for the life of the process and every click is dropped (issue #74, decisions/0029)")
	}
	if release < launch {
		t.Fatal("the launch slot is released on the calling thread rather than from the goroutine: it would be free again before the launch it bounds has finished, so the bound counts nothing")
	}

	// And the apartment the launch needs is entered on the goroutine's own
	// thread, because a fresh goroutine is in none and ShellExecuteW can activate
	// a COM handler.
	worker := externalOpenSource(t, source, "func (host *Host) shellExecuteOpen(", "")
	for _, want := range []string{"runtime.LockOSThread()", "windows.CoInitializeEx(", "windows.CoUninitialize()"} {
		if !strings.Contains(worker, want) {
			t.Fatalf("shellExecuteOpen no longer contains %q", want)
		}
	}
	// S_FALSE - the thread was already in a compatible apartment - arrives as
	// ERROR_INVALID_FUNCTION and still owes a CoUninitialize, which is why the
	// balance is claimed for it too. Narrowing that condition to err == nil
	// leaks an apartment per launch, and no headless test can reach the branch
	// to notice: CoInitializeEx has to fail on the worker for it to run.
	if !strings.Contains(worker, "ERROR_INVALID_FUNCTION") {
		t.Fatal("shellExecuteOpen no longer balances the S_FALSE case: a thread already in a compatible apartment still owes a CoUninitialize (decisions/0029)")
	}

	// One call site for the syscall, so the check above covers every path to it.
	if got := strings.Count(source, "procShellExecute.Call("); got != 1 {
		t.Fatalf("procShellExecute is called %d times in this file, want exactly 1 - the guard above reads one function", got)
	}
}

// externalOpenSource returns the source between two literals, and fails loudly if
// either has moved: a rename that silently emptied the guard above would leave
// its assertions trivially true. An empty close means "to the end of the file".
func externalOpenSource(t *testing.T, source, open, close string) string {
	t.Helper()
	start := strings.Index(source, open)
	if start < 0 {
		t.Fatalf("%q not found in host/systembrowser_windows.go - this guard is scoped by that literal", open)
	}
	body := source[start:]
	if close == "" {
		return body
	}
	end := strings.Index(body, close)
	if end < 0 {
		t.Fatalf("%q not found after it - the guard cannot tell where the function ends", close)
	}
	return body[:end]
}
