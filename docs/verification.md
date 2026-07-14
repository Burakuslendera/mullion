# Verification and Acceptance

## Contents

- [1. Automated gates](#1-automated-gates)
- [2. Why "it compiles" is not acceptance](#2-why-it-compiles-is-not-acceptance)
- [3. Manual acceptance checklist](#3-manual-acceptance-checklist)
- [4. Traps when scripting GUI checks](#4-traps-when-scripting-gui-checks)
- [5. Diagnostic build tags and env switches](#5-diagnostic-build-tags-and-env-switches)
- [6. What a good bug report contains](#6-what-a-good-bug-report-contains)

How a change to `mullion` is proved correct. The automated gates are cheap and
catch a narrow class of mistakes; the manual gates are the only thing that
catches the class of mistakes this library actually produces. Both are
mandatory before a frame, hit-test, DPI or WebView2 change is considered done.

## 1. Automated gates

Run in this order. Each one exists because it catches something the previous
one cannot.

```
gofmt -l .                                       # must print nothing
go build ./...                                   # windows build
go vet ./...                                     # syscall/unsafe/printf misuse
go test ./...                                    # unit + table tests
go test -race ./...                              # message pump vs. callback races
go build -tags mullion_dwm_caption_diag ./...    # diagnostic tag still compiles
go build -tags mullion_caption_passthrough_diag ./...
GOOS=linux go build ./...                        # non-windows stub gate
pwsh scripts/leak-scan.ps1                       # nothing private is published
cd examples/basic && go run .                    # it actually starts
```

| Gate | What it catches |
| --- | --- |
| `gofmt -l .` | Formatting drift. Non-empty output is a failure, not a suggestion. |
| `go build ./...` | Compile errors on the default Windows path only. |
| `go vet ./...` | Misuse of `unsafe.Pointer` around Win32 calls, wrong printf verbs in log lines, suspicious struct tags. Vet is the closest thing to a static check on syscall boundaries. |
| `go test ./...` | Pure-logic invariants: hit-test region maths, non-client rect adjustment, DPI scaling, style-bit composition, asset MIME/range handling, diagnostic log parsing — **and every COM vtable offset and IID in `internal/webview2`** (see below). |
| `TestNoNetworkListener` | The promise on the README's first screen: **no local port is ever opened.** It greps the source for `net.Listen`, `http.ListenAndServe`, `httptest` and loopback literals. Until this existed the claim was documentation and nothing else — the kind of invariant that decays quietly, when somebody reaches for a test server "just for a fixture" and the build stays green. See [decisions/0002](./decisions/0002-no-local-port.md). |
| `TestNoUpstreamBrandLeak`, `TestNoNonASCIIInSource` | The repository stays in one language, and carries nothing from the private code base it was extracted from. |
| `go test -race ./...` | Data races between the UI thread and any goroutine that touches shared state (asset serving, watchdogs, bound callbacks). The window procedure runs on one thread; anything reachable from another must be synchronised, and the race detector is the only automated proof of it. |
| `go build -tags <diag>` | Diagnostic builds rot silently. A plain `go build ./...` never compiles a file behind a build tag, so a rename in the default path can break a diagnostic variant for weeks without anyone noticing. Each diagnostic tag gets its own build in CI. |
| `GOOS=linux go build ./...` | The non-Windows stubs still satisfy the public API. Anyone who imports this package from a cross-platform program must be able to compile on Linux/macOS, even though the window cannot run there. |
| `pwsh scripts/leak-scan.ps1` | Anything that must never be published: upstream product names, absolute local paths, artefact hashes, real-looking pseudo-versions, commit-trailer text inside a file — across tracked files **and commit messages**. CI runs it in the Windows job. |
| `go run .` in the example | The bootstrap sequence still produces a visible window with a loaded frontend. Compilation says nothing about this. |

### The COM ABI is pinned by tests, because the compiler cannot see it

`internal/webview2` is a hand-written COM binding, and a COM call is a jump through
slot *n* of a vtable. Go's type checker cannot tell a correct vtable struct from a
wrong one: both compile. A field inserted, dropped or transposed silently retargets
every method after it, and the result is not `E_NOINTERFACE` — it is a call through
the wrong function pointer, i.e. a crash or memory corruption inside the browser
process, at the point of first use. **A green build proves nothing about a vtable.**

The gate is therefore a test, and it must stay one:

- Every vtable's slot offsets are pinned with `unsafe.Offsetof`, every interface ID is
  pinned byte for byte, and the settings chain's total slot count (39) is asserted
  against the sum of its links. Change a struct in `interfaces_windows.go` and the test
  tells you immediately; ship it untested and the user finds out.
- These tests need **no WebView2 runtime and no window** — they are assertions about
  struct layout — so they run in the same headless suite as everything else. Keep them
  there.
- The reference for any change is Microsoft's MIDL-generated `WebView2.h` / `WebView2.idl`
  from the SDK package. **The MS Learn reference pages list members alphabetically and
  must never be used to derive an ABI.**

### The test suite is headless — keep it that way

No test in this package creates an `HWND`, calls `Run()`, spins a message pump,
or requires a WebView2 Runtime to be installed. That is a deliberate design
constraint, not an accident: it means the full suite (including `-race` and
every diagnostic tag) runs on a headless CI worker with no desktop session, no
GPU and no WebView2 install.

Rules for new tests:

- Test the **pure function**, not the window. Hit-test decisions, rect maths,
  style-bit composition, DPI conversion and log-line parsing are all expressed
  as functions over plain structs precisely so they can be tested this way. If
  a new behaviour is hard to test without a window, that is a signal the logic
  should be extracted out of the window procedure.
- Never call a Win32 entry point from a test; tests exercise the callers of the
  Win32 wrappers, not the wrappers.
- If a test needs a window to be meaningful, it belongs in the manual checklist
  below — and the code it would have tested belongs in a function that *can* be
  tested headlessly. A test that requires a desktop turns a green CI into a
  machine-dependent coin flip. Reject it in review.

## 2. Why "it compiles" is not acceptance

In this architecture the compiler is nearly blind to the failure modes that
matter. All of the following build cleanly, pass every unit test, and are
broken:

- The window opens and paints **white forever** — the frontend never loaded:
  the asset handler rejected the request, the synthetic origin was mismatched,
  or the WebView2 controller was created but never made visible.
- The window is **visible but dead to drag** — the title bar returns the wrong
  hit-test code, or the WebView2 child swallows the pointer before the parent
  sees it.
- **Resize borders show the right cursor but do not resize** — the cursor comes
  from one code path and the sizing from another; they can disagree.
- The **system menu opens with the wrong item states** — `Maximize` enabled
  while already maximized, `Restore` greyed out while maximized.
- Everything works until the window crosses onto a **monitor with a different
  scale factor**, where the non-client geometry is still computed at the old DPI.

None of these are compile errors, and none are unit-testable without a window.
Acceptance therefore means **live interaction with a running window**, by a human
or by a script that drives the native frame. Green build plus green tests is the
entry ticket to acceptance, not acceptance itself.

## 3. Manual acceptance checklist

Run `examples/basic` (or the host application) and walk the list. Every item is
a pass/fail with an observable result — "looks fine" is not a result.

- [ ] **Title bar drag.** Press in the title bar and move: the window follows
      the cursor immediately, with no dead zone and no snap-back.
- [ ] **Double-click title bar.** Toggles maximize, then toggles back.
- [ ] **Drag down from maximized.** Press in the title bar of a maximized
      window and drag downward: the window restores *under the cursor* and
      continues following it in the same gesture — it must not restore to a
      corner or drop the drag.
- [ ] **Resize: 4 edges and 4 corners.** For each of the eight zones, check
      **both** that the cursor changes to the correct shape *and* that dragging
      actually resizes in that direction. The cursor and the sizing come from
      different code paths; test them as two separate assertions.
- [ ] **Minimize** from the custom caption control, and restore from the
      taskbar.
- [ ] **Close** from the custom caption control; the process exits and no
      child process is left behind.
- [ ] **Right-click the title bar → system menu appears**, and its item states
      are correct **in both window states**:
      restored → `Restore` disabled, `Maximize` enabled, `Move`/`Size` enabled;
      maximized → `Restore` enabled, `Maximize` disabled, `Move`/`Size`
      disabled. This is the single most fragile item on the list: it breaks
      whenever style bits or the non-client path change, and it breaks silently
      because the menu still *opens*. Check it every time.
- [ ] **`Win`+`←` / `Win`+`→` snap.** The window snaps to the half-screen work
      area (not the full monitor rect — the taskbar must still be visible), and
      snapping back out restores the previous size.
- [ ] **Mixed-DPI monitor transition.** With two monitors at different scale
      factors, drag the window across the boundary. The title bar height,
      caption controls, resize borders and frontend text must all rescale, and
      the client area must not be a stretched bitmap. Then maximize on each
      monitor and confirm the client area fills the work area exactly, with no
      title bar clipped off the top.
- [ ] **Hit-test trace.** Relaunch with `MULLION_HITTEST_DIAG=1` and repeat the
      drag / resize / caption-button passes. The emitted hit-test lines must
      show the expected code for each region (caption over the drag strip,
      the eight sizing codes over the borders and corners, client over the
      frontend). A visually correct window with wrong hit-test codes is a
      latent bug, and this is how you see it.
- [ ] **Tooltip trace** (when touching caption-control tooltips): relaunch with
      `MULLION_TOOLTIP_TRACE=1` and confirm show/hide events pair up and no
      tooltip is orphaned after the pointer leaves the window.

If a change touches the frame, DPI or hit-test code, the whole list is re-run.
There is no "small frame change".

## 4. Traps when scripting GUI checks

Automating the manual list is possible but the environment fights back. These
are the failure modes that produced false passes and false failures, and the
rules that avoid them.

**Injected mouse input does not reach the WebView2 child.** Synthetic input
(`mouse_event`, `SendInput` at a `SetCursorPos` location) is delivered to the
native window tree, and the WebView2 child does not process it the way it
processes real hardware input. Consequence: **you cannot click an HTML button
from a script.** What you *can* drive is the native frame — the title bar drag
strip, the resize borders and corners, and the caption buttons — because those
live on the parent `HWND` and are resolved by our own window procedure. So:
script the native frame; do not script the DOM. To verify the frontend/host
bridge instead, have the frontend call one host binding on load and write the
result into the DOM, then assert on the screenshot or on the host-side log.

**Do not measure WebView2 client coverage with "the largest child HWND".**
Chromium creates an intermediate compositing window in addition to the
controller window. After a programmatic resize that intermediate window can
report a **stale rect larger than the client area** for a while — the parent
clips it, so there is no visual defect, but a script comparing "largest child"
against the client rect will report a bogus failure. Enumerate children and
measure **only the controller child** (`Chrome_WidgetWin*` class); ignore the
rest.

**Never run cursor/foreground smokes in parallel.** The cursor position and the
foreground window are *global* machine state. Two scripts that move the mouse or
raise a window at the same time corrupt each other and produce failures that do
not reproduce. Serialise every check that drags, hovers, snaps or focuses. Only
checks that are purely passive (log scraping, build gates) may run concurrently.

**Screenshot acceptance has a contract.** A screenshot is evidence only if all
four hold:
1. the capturing probe is **DPI-aware** (otherwise Windows hands it a scaled,
   blurry bitmap and every pixel assertion is meaningless);
2. the target window is found by its **real window class**, not by "the
   foreground window" or "the biggest window";
3. the capture waits for the **frontend-ready signal** — capturing during load
   photographs a white client area and proves nothing;
4. the crop includes an **outer margin** beyond the window rect, so shadow,
   rounded corners and any leaked native caption are inside the frame.

**Quit gracefully, then clean up.** Post the application's own quit message to
the window first and give it a moment to tear itself down; only then walk the
process tree and force-stop anything left. Force-stopping the process tree
directly kills the WebView2 browser process out from under the controller and
produces teardown error output — noise that looks exactly like a real bug.

**Check the return value of `PostMessageW`.** It fails silently. A script that
posts a quit or a click and never inspects the result will happily report a
clean lifecycle for a message that was never delivered.

**Do not trust the PID you launched.** An application may hand off to another
process (relaunch, elevation, single-instance handoff), so the PID your script
started can exit immediately while the real window belongs to a different
process. Find the window by **class name**, then derive the PID from the window
— not the other way around. When cleaning up, stop only the process tree you
own; if a window with your class is running that you did not start, abort the
run instead of killing someone else's process.

## 5. Diagnostic build tags and env switches

Diagnostics exist because the frame bugs in this library are *invisible* — the
window looks right and behaves wrong. Each switch trades a little runtime cost
or a little behaviour for a lot of visibility.

| Switch | Kind | What it does | When to turn it on |
| --- | --- | --- | --- |
| `mullion_dwm_caption_diag` | build tag | Builds an alternative caption/DWM extension path and logs the frame decisions it makes, so the default path can be compared side by side against it. | Double title bar, missing or extra shadow, wrong rounded corners, native caption leaking during startup, maximize glyph flicker. |
| `mullion_caption_passthrough_diag` | build tag | Builds a variant of the caption hit-test/passthrough behaviour and traces which component claims each caption-area point. | Drag works but caption buttons do not (or the reverse), snap layouts flyout does not appear on hover, hover state stuck after the pointer leaves. |
| `MULLION_HITTEST_DIAG=1` | env | Emits one line per hit-test decision: point, region, returned code. | Any drag/resize/cursor complaint; mandatory when changing hit-test geometry. |
| `MULLION_TOOLTIP_TRACE=1` | env | Traces caption-control tooltip show/hide/lifetime. | Tooltips that stick, never appear, or appear on the wrong control. |

Rules:

- A diagnostic tag is a **diagnostic**, never a release configuration. Ship the
  default path; use tags to find out why the default path is wrong.
- **Diagnostic builds must be compiled in CI.** `go build ./...` does not touch
  a single file behind a build tag, so a diagnostic variant can be broken by an
  unrelated rename and stay broken until the day you need it — which is
  precisely the day you cannot afford to fix it first. Every tag gets a
  `go build -tags <tag> ./...` line in the gate list above. The same holds for
  tests: a tag that has its own test files needs
  `go test -tags <tag> ./...` too.
- Env switches must default to **off** and must not change behaviour when on —
  only logging. If enabling a diagnostic makes the bug disappear, the
  diagnostic is not read-only and is itself a bug.

## 6. What a good bug report contains

Frame bugs are environment-dependent; a report without environment is a report
that cannot be reproduced.

**Do not gather the environment by hand. Run it:**

```
go run github.com/Burakuslendera/mullion/cmd/mullion@latest doctor   # no checkout needed
go run -buildvcs=true ./cmd/mullion doctor                           # from a checkout
go install ./cmd/mullion                                             # keep it: $(go env GOPATH)/bin
```

It prints a paste-ready block: Windows build (corrected — the registry still says
"Windows 10" on Windows 11), GPUs, every monitor with its **physical** resolution,
scaling and work area, and the WebView2 runtime — not the one the registry
advertises, but **the one mullion would actually load**, together with whether it
still exports the entry point the host calls. Exit code 0 means mullion can start
on that machine. See [decisions/0008](./decisions/0008-doctor-is-a-go-command.md).

The monitor section is why this is a command rather than a checklist. Windows
reports a *virtualised* resolution to a process that is not DPI-aware, so a
reporter reading their own settings panel writes "1536x864" for a 1920x1080
monitor at 125% — and the reader spends an afternoon chasing a scaling bug that
was never there. The command declares per-monitor awareness before it measures.

Mind the `-buildvcs=true` from a checkout: `go run` does not stamp the revision
into the binary, so without it the version line reads a bare `devel` and
identifies nothing. `go install` and `go build` do stamp it. The report says so
when it happens rather than letting the line pass as an answer.

**The build identifies itself.** `Run` logs `mullion: version=…` at startup, read
out of the binary's own build info: a tag (`v0.1.0`), a pseudo-version carrying
the commit hash (`v0.0.0-20060102150405-abcdef123456`), a `devel` build with its
revision, or a disclosed `replace` directive. A report that includes the log has
already answered "which commit" — and answered it more reliably than the reporter
could from memory.

Then include:

- **What you did, in frame terms** ("dragged down from maximized on the
  secondary 150% monitor and released over the primary"), and **expected vs.
  observed** stated as an observable — cursor shape, window rect, which control
  responded — not as an impression.
- **The relevant log lines**, not the whole log: the hit-test lines around the
  failing gesture (`MULLION_HITTEST_DIAG=1`), plus any warning or error from the
  host or the WebView2 layer in the same window of time.
- **Which switches were on** — build tags used, env variables set. A trace taken
  from a diagnostic build must say so.
- **WebView2 Runtime version — and which runtime was actually loaded.** A large
  share of "works on my machine" in a WebView2 host is a runtime-version
  difference. mullion discovers the runtime itself (registry, or a
  `WEBVIEW2_BROWSER_EXECUTABLE_FOLDER` pin) and loads the runtime's own DLL, so
  the version alone is not the whole answer: say whether that environment
  variable was set, and give the resolved runtime path if the host logs it. A
  report against a pinned fixed-version runtime is a different report.
- **Windows build**, and whether the session was a normal desktop, a remote
  session or a VM — remote sessions change DWM composition and can invalidate
  visual findings.
- **Monitor setup, generically**: how many monitors, the scale factor of each,
  which is primary, and where the window was when it failed. Say explicitly
  whether the scale factors differ; mixed-DPI is its own bug class.
- **Repro steps from a cold launch**, with a hit rate if intermittent. "3 of 10
  launches" is useful; "sometimes" is not.

A report that lets someone else reproduce the failure on the first try is worth
more than a patch.

> Last updated: 2026-07-14 | Editor: Claude (Fable 5) | Change: add the leak-scan gate to the automated gates; make the example pseudo-version obviously synthetic.
