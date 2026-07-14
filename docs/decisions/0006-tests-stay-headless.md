# 0006. No test creates a window

**Status:** Accepted

## Context

The natural test of a window library opens a window. It is also the test that
cannot run: a CI worker has no desktop session, no GPU and no WebView2 Runtime
installed, and a suite that needs those runs nowhere except the machine of the
person who wrote it.

The temptation is real, because this library's bugs are live bugs - a four-pixel
dead band in the title bar, a window that paints white forever, a system menu with
stale item states. A test that opened a window would appear to catch them.

## Decision

No test calls `Run()`, creates an `HWND`, spins a message pump, or requires a
WebView2 Runtime. This is a **design constraint on the code**, not merely a rule
for tests: hit-test geometry, `WM_NCCALCSIZE` rect maths, DPI conversion, style-bit
composition, asset resolution, bridge routing, log-line formats and every COM
vtable offset are expressed as pure functions over plain structs, precisely so
that they can be asserted without a window. If a new behaviour is hard to test
headlessly, that is a signal to lift it out of the window procedure.

## Alternatives rejected

**Integration tests that drive a real window.** This is the honest test, and it
would exercise the thing that actually ships. It fails on its own terms: injected
mouse input (`SetCursorPos`, `mouse_event`, `SendInput`) **does not reach the
WebView2 child window at all** - not flakily, at all - so the DOM half of the
product is unautomatable regardless of how much desktop the runner has. What
remains automatable is the native frame, and that still needs an interactive
session, a WebView2 install, and serialised access to global cursor and foreground
state. A green CI that depends on those is a coin flip that looks like a fact.

**Mock COM behind an interface and test the window procedure against it.** It
would keep the message routing under test, and the seam is not hard to build. But
the failures in this architecture are ABI failures and shell-behaviour failures. A
mock reproduces what we already believe; it cannot disagree with us, and the whole
value of a test here is that it can.

## Consequences

**Live behaviour cannot be proved by the test suite, and the suite must not be
read as if it could.** Everything in the list above - white window, dead drag
band, wrong hit-test code, stale menu state, mixed-DPI geometry - passes every
test and fails in front of the user.

The compensating machinery is therefore mandatory, not optional:
`docs/verification.md` carries a manual acceptance checklist that is re-run in
full whenever the frame, the hit test, DPI or snap is touched, and the pull
request template requires a reviewer to be told what was verified live and on what
display setup. "It compiles" is not acceptance, and this record is what makes that
sentence enforceable rather than a slogan.

The standing cost: any behaviour that resists extraction from the window procedure
either gets extracted anyway, or ships untested and named as such.

## What would change our mind

A way to deliver input the WebView2 child actually processes - a WebView2
automation surface, or an injection path that reaches the browser child - together
with a CI worker that has a real desktop session and a runtime. Then a
window-driving suite could be added **alongside** the headless one. It would not
replace it: the headless suite is what runs on a machine with no browser, and the
COM ABI tests in particular have to keep running there, because they are what
catches a vtable mistake before it reaches a user.

## Evidence

- `docs/verification.md`, "The test suite is headless - keep it that way", and the
  manual acceptance checklist it exists to justify.
- `.github/PULL_REQUEST_TEMPLATE.md`: "No test creates a window, an HWND, or needs
  the WebView2 Runtime. The suite stays headless."
- `internal/webview2/interfaces_windows_test.go`: the riskiest code in the
  repository is tested by reading struct layout, with no runtime present. The two
  loader tests that do look at the machine skip when nothing is installed.
- `docs/lessons-and-dead-ends.md` section 11: the finding that injected input never
  arrives at the WebView2 child, and the resulting split between what is
  automatable and what is not.
