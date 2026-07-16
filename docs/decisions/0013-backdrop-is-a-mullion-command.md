# 0013. The screenshot backdrop is a mullion command

**Status:** Accepted

## Context

A published window screenshot needs a margin around the window — the DWM
shadow and the rounded corners are exactly what a frame change breaks
(docs/verification.md §4) — and a margin captures whatever the desktop shows
behind the window. For a public repository that is a disclosure problem before
it is an aesthetic one.

`scripts/screenshot.ps1 -Backdrop` solves it for the scripted path: it covers
the monitor with a flat grey for the duration of its own capture. But that
solves it only inside that script — it needs a checkout and PowerShell, and it
cannot help someone capturing with the tool they already use (Win+Shift+S,
ShareX, anything). The maintainer's request was exactly that manual flow: a
backdrop they can raise themselves, with nothing driving it.

## Decision

`mullion backdrop` is a subcommand of `cmd/mullion`, beside `doctor` and for
the same reason doctor is a Go command (0008): anyone with the library has a Go
toolchain, so

```
go run github.com/Burakuslendera/mullion/cmd/mullion@latest backdrop
```

works with no checkout and no PowerShell. It covers the whole virtual screen —
every monitor — with one flat-colour Win32 popup (`internal/backdrop`), blocks
until dismissed, and exits 0. If a visible window of the target class
(`MullionWindow`; `-class` overrides, empty skips) exists, the backdrop slots
itself directly underneath it and lifts it to the top — two `SWP_NOACTIVATE`
z-order moves, so no foreground-steal restriction applies and the window to
capture is in front the moment the backdrop opens. The lifted window is then
watched (a 200ms `WM_TIMER` checking `IsWindow`/`IsWindowVisible`/`IsIconic`):
move and resize it freely, but close it, end its process, or minimise it and
the backdrop closes itself — the session ends when the subject leaves the
stage. `-colour #rrggbb` overrides the default dark grey; the parse is strict,
is the command's entire input surface, and is tested headlessly. The window
half follows the repository's window rules: per-monitor-v2 declared before the
HWND exists, a locked OS thread for the message loop, one `NewCallback` at
package init.

Three properties are deliberate and security-motivated:

- **Not topmost.** Any window the user raises sits above it, so the backdrop
  can never hold the desktop hostage, and the window being captured needs only
  a click or an Alt+Tab to come forward.
- **Three independent exits:** Esc on the window, `WM_CLOSE` (Alt+F4, the
  taskbar button), and Ctrl+C on the terminal that started it.
- **Visible in the taskbar** (`WS_EX_APPWINDOW`): a full-screen window that
  hides from the taskbar is the shape of a decoy; this one is always
  discoverable and closeable.

It opens no socket, writes no file, and logs nothing but a fixed usage line.
The command's scope widens `cmd/mullion` from "diagnostics" to "diagnostics
and capture helpers"; the usage header says so.

## Alternatives rejected

**Leave it inside `scripts/screenshot.ps1` only.** Already shipped, and stays.
Rejected as the *only* home because it composes with nothing: the backdrop
exists solely inside that script's capture, and the manual flow — the one that
was actually asked for — gets nothing.

**A topmost backdrop.** Simpler to reason about (nothing can appear over it)
and it is what the PowerShell path does under its own control. Rejected here:
an unattended topmost full-screen window is a lockout if its owner
misbehaves, and it would sit *over* the very window being captured, which the
scripted path solves with a z-order dance the manual flow cannot perform.

**A second PowerShell script.** Same language as the existing tooling, no new
Go surface. Rejected: it still needs a checkout and pwsh, and it would be the
second copy of the backdrop (one WinForms, one script) with no user the first
copy does not already serve.

**A library API (`host.Backdrop()`).** Rejected outright: the public package
is a compatibility promise (every addition is permanent), and a capture aid is
tooling, not window hosting. A subcommand can be retired; an exported API
cannot.

## Consequences

- `cmd/mullion` is no longer diagnostics-only. That is a real scope widening,
  recorded here and in the usage text, and every future "should this be a
  mullion command?" starts from this precedent — the bar stays: no checkout
  assumed, no state written, Windows-gated behind the 0007 stub pattern.
- The window half is not covered by tests — no test may create a window
  (0006) — so any change to `backdrop_windows.go` owes a live check: raised,
  covered every monitor, dismissed by all three exits. The colour parse is the
  tested half.
- The not-topmost choice means the backdrop does not guarantee it covers
  everything at all times: whatever the user raises is above it. That is the
  point, and it is documented behaviour, not a bug to fix.

## What would change our mind

- If the manual flow goes unused and captures all happen through
  `scripts/screenshot.ps1`, the command is a dead surface and should be
  retired before anything depends on it.
- A third helper of this kind is the trigger to reconsider whether
  `cmd/mullion` should stay one flat command or the helpers should move out.
- A Windows capture facility that masks the desktop natively would make this
  redundant.

## Evidence

- `internal/backdrop`: `ParseColour` with `TestParseColourAcceptsOnlySixHexDigits`
  and `TestDefaultHexParses` (headless); `backdrop_windows.go` (window half);
  `backdrop_other.go` (0007-pattern stub).
- Live check on this machine, 2026-07-16: raised over a real two-monitor
  desktop (one window spanning the 3840x1080 virtual screen), the covered
  screen captured and 500 sampled pixels all measured as the configured
  colour, then dismissed via `WM_CLOSE` and via Esc in separate runs, exit 0
  both times. The lift: with `examples/basic` open, the backdrop was raised
  and — with nothing touching the z-order afterwards — a capture measured the
  window's pixels in front and backdrop grey beside it. The watch, all three
  ways out, measured live: the demo window closed normally, minimised
  (`SW_MINIMIZE`), and its process killed from a terminal — the backdrop
  closed itself within its timer tick in each run.
- The scripted sibling and the need that produced it: `a7c689c`
  (`screenshot.ps1 -Backdrop`), and the maintainer's request for the same
  ground under a hand-driven capture tool.
