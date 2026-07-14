# Snap, Non-Client Regions, and the Custom Title Bar Problem

## Contents

- [1. The core obstacle](#1-the-core-obstacle)
- [2. What non-client region support actually does](#2-what-non-client-region-support-actually-does)
- [3. `NonClientRegionKind` value map](#3-nonclientregionkind-value-map)
- [4. Runtime and SDK matrix, and hand-binding a COM interface](#4-runtime-and-sdk-matrix-and-hand-binding-a-com-interface)
- [5. Activation timing (critical)](#5-activation-timing-critical)
- [6. What actually gives you Snap](#6-what-actually-gives-you-snap)
- [7. Decision table](#7-decision-table)
- [8. DWM caption theming (recolour the caption without breaking Snap)](#8-dwm-caption-theming-recolour-the-caption-without-breaking-snap)
- [9. The system menu with a custom title bar](#9-the-system-menu-with-a-custom-title-bar)
- [10. Sources](#10-sources)

A field report on hosting WebView2 in a Win32 window with a custom (HTML) title bar
while keeping Windows 11 Snap behaviour. Everything below is either quoted from
primary documentation or was observed live against a real WebView2 Runtime. Where a
claim was **tested and disproved**, it says so.

---

## 1. The core obstacle

**Windows 11 opens the Snap-layouts flyout only when the application returns
`HTMAXBUTTON` from `WM_NCHITTEST` for the point under the cursor. WebView2 lives in a
separate child HWND that covers the client area, so the child swallows the hit-test
and the parent never gets asked — the parent therefore cannot return `HTMAXBUTTON`
over the pixels where your HTML maximize button is drawn.**

That single sentence explains every hack in this space: caption bands, region holes,
layered overlays, hover forwarding, synthetic hit-tests. They are all attempts to get
`HTMAXBUTTON` back out of a window whose client area is owned by someone else. The
flyout is not triggered by a keypress or an API call — it is triggered by the
compositor hover-testing your window. No hit-test, no flyout.

---

## 2. What non-client region support actually does

`ICoreWebView2Settings9.IsNonClientRegionSupportEnabled = TRUE` turns on the CSS
`app-region: drag | no-drag` property inside the WebView. It is the officially
supported way to punch non-client behaviour through the child HWND, it works, and it
is the foundation of the whole approach. But it produces **exactly one kind of
non-client region: caption.**

- An `app-region: drag` element reports `NonClientRegionKind.Caption` (2 = `HTCAPTION`),
  which gives you **window dragging**, **right-click system menu** and **double-click
  maximize/restore**.
- It does **not** give you a maximize *button*. There is **no CSS property, no
  attribute, no element role that makes an HTML element report
  `NonClientRegionKind.Maximize` (9 = `HTMAXBUTTON`).** The Minimize/Maximize/Close
  kinds exist in the enum because they describe the **Window Controls Overlay**
  buttons that WebView2 itself draws — not anything your page can mark.

Therefore: **`IsNonClientRegionSupportEnabled` + `app-region: drag` does not give you
the Snap hover flyout.** This is the most common misconception about the API, and it
survives because the documentation never states the negative — the settings page lists
drag, system menu and double-click, and simply stops.

Confirmed live, not inferred. With non-client region support enabled against a current
runtime, on a frameless window with an HTML title bar:

| Behaviour | Result |
|---|---|
| Drag the window by an `app-region: drag` area | **yes** |
| Double-click that area to maximize/restore | **yes** |
| Right-click that area for the system menu | **yes**, but only if the window still has `WS_SYSMENU` (see §9) |
| Hover the HTML maximize button → Snap flyout | **no** — no `HTMAXBUTTON` is ever produced |

The only WebView2-native path to a real maximize button is the **Window Controls
Overlay** (WebView2 draws its own native min/max/close), and Microsoft is explicit
that WCO is used *together with* `app-region`, not instead of it. WCO has been
pre-release for a long time and needs a newer runtime and SDK — a shipping risk for an
application that must run on whatever runtime the user happens to have. Binding it is
the easy part (§4); *depending* on it is the risk.

---

## 3. `NonClientRegionKind` value map

The values are deliberately aligned with `WM_NCHITTEST` return codes:

| Kind | Value | `WM_NCHITTEST` equivalent | Producible from HTML? |
|---|---|---|---|
| `Nowhere` | 0 | `HTNOWHERE` | — |
| `Client` | 1 | `HTCLIENT` | default (and `app-region: no-drag`) |
| `Caption` | 2 | `HTCAPTION` | **yes** — `app-region: drag` |
| `Minimize` | 8 | `HTMINBUTTON` | no — WCO only |
| `Maximize` | 9 | `HTMAXBUTTON` | **no** — WCO only |
| `Close` | 20 | `HTCLOSE` | no — WCO only |

Read that table next to §1 and the problem is stated completely: the one value you
need is the one value you cannot produce.

---

## 4. Runtime and SDK matrix, and hand-binding a COM interface

- **Runtime:** the non-client region API is stable from **WebView2 Runtime
  131.0.2903.40** onward. Older runtimes will fail the capability check; treat that as
  a recoverable condition, not an error.
- **SDK:** `ICoreWebView2Settings9` first appears in the 1.0.2420.47 SDK (1.0.2415
  pre-release).
- **Go:** the third-party Go bindings in general circulation wrap `ICoreWebView2Settings`
  through `…Settings6` and stop. `Settings9` is **not** in their settings vtable — and
  this is still true on their main branches, so **bumping a dependency does not help.**
  You must bind the interface yourself. This library ends up binding the whole surface
  itself (`internal/webview2`, see `docs/architecture.md`), and this interface is why
  that road opened: needing one COM interface a binding does not cover leaves you with a
  fork, a raw-COM escape hatch inside someone else's abstraction, or your own binding.

Hand-binding a COM interface in CGo-free Go is a three-part recipe. It is mechanical,
but two of the three parts crash instead of returning an error when you get them wrong.

**(a) The IID.** `ICoreWebView2Settings9` is
`{0528a73b-e92d-49f4-927a-e547dddaa37d}`. Decompose it into a `windows.GUID`
carefully — `Data4` is a byte array, not a number.

**(b) The vtable layout — the part that kills you.** A COM object's first machine word
is a pointer to its vtable; you call method *n* by indexing slot *n*. There is no type
checking and no bounds checking. A wrong offset does not return `E_NOINTERFACE`; it
calls a different function with your arguments and takes the process down.

> **Derive the layout from the SDK header, never from a reference page.** The
> authoritative source is Microsoft's `WebView2.h` (and `WebView2.idl`) out of the
> `Microsoft.Web.WebView2` package: it is MIDL-generated, so its declaration order *is*
> the ABI. **The MS Learn reference pages list an interface's members alphabetically.**
> They are documentation, not an ABI, and reading a vtable off one produces a layout
> that compiles, passes review, and calls the wrong function pointer at runtime.

Derive the layout by walking the inheritance chain and counting methods, in order:

```
IUnknown                       3   QueryInterface, AddRef, Release
ICoreWebView2Settings         18   (9 properties, get+put)
ICoreWebView2Settings2         2   UserAgent
ICoreWebView2Settings3         2   AreBrowserAcceleratorKeysEnabled
ICoreWebView2Settings4         4   IsPasswordAutosaveEnabled, IsGeneralAutofillEnabled
ICoreWebView2Settings5         2   IsPinchZoomEnabled
ICoreWebView2Settings6         2   IsSwipeNavigationEnabled
ICoreWebView2Settings7         2   HiddenPdfToolbarItems
ICoreWebView2Settings8         2   IsReputationCheckingRequired
ICoreWebView2Settings9         2   IsNonClientRegionSupportEnabled
                              ---
                              39   slots  →  Get_ = 37, Put_ = 38
```

**`Settings4` contributes four slots, not two.** It carries *two* properties —
`IsPasswordAutosaveEnabled` and `IsGeneralAutofillEnabled`. The natural assumption that
each revision adds exactly one property is wrong exactly once in this chain, and being
wrong here shifts `Settings5` through `Settings9` by two slots — which means the call
you make to enable non-client region support lands somewhere else entirely, silently.
`Settings5` is pinch zoom and `Settings6` is swipe navigation, **not the other way
round**; that pair is easy to transpose and equally fatal.

Declare the vtable as a Go struct with one function-pointer field per slot, in exactly
that order, and **lock it with a unit test** using `unsafe.Offsetof` — that turns a
future runtime crash into a compile-time-ish test failure. Do the same for the IID.

Three more ABI details, all verified against a live runtime, all of the same kind — the
sort that a plausible-looking binding gets wrong and only discovers as a crash:

- **`ICoreWebView2Controller`: `IsVisible` comes *before* `Bounds`.** Declaring the
  bounds pair first is the intuitive grouping and shifts every slot after it.
- **`ICoreWebView2WebResourceResponse`: `get_Headers` sits *between* `Content` and
  `StatusCode`** — not at the end, where a "content, status, reason, headers" grouping
  would put it. Getting it wrong shifts three slots.
- **Aggregate arguments follow the x64 convention, not the C signature.** `put_Bounds`
  is declared as taking a `RECT` **by value**, but a `RECT` is 16 bytes, and on x64
  "structs of size 8, 16, 32 or 64 *bits* are passed as integers of the same size;
  structs of other sizes are passed as a pointer to memory allocated by the caller." So
  `put_Bounds` is called with a **pointer**. `COREWEBVIEW2_COLOR` is 4 bytes and really
  does travel by value, packed into a register. And `put_RasterizationScale(double)`
  wants its argument in `XMM1`, so the bits must be passed with `math.Float64bits` —
  converting the float to a `uintptr` numerically sends a truncated integer instead.

**(c) `QueryInterface` from the base pointer.** Take the `*ICoreWebView2Settings` you
already hold, reinterpret it as an `IUnknown` (first word = vtable), call
`QueryInterface` with the Settings9 IID, and `Release` the result when done. The base
pointer is borrowed — do **not** release it, or you will over-release an object you do
not own. This is also the *only* correct way to ask whether the running runtime supports
non-client regions: the version floor that `WebView2Loader.dll` enforces does not exist
inside the runtime DLL, so a version comparison proves nothing (see
`docs/architecture.md`). Ask the object, not the version number.

**A trap worth naming:** auto-generated COM bindings in this ecosystem sometimes pass
`BOOL` properties by *address* (`&goBool`). A Win32 `BOOL` is a 4-byte int passed **by
value**; passing a pointer means even `false` arrives as a non-zero pointer value, i.e.
`TRUE`. Pass `1`/`0` by value on `Put`, and read into an `int32` on `Get`.

---

## 5. Activation timing (critical)

`IsNonClientRegionSupportEnabled` **takes effect on the next navigation.** That fact
dictates exactly one correct sequence:

```
create window → embed WebView2 → [ enable non-client region support ]
                                 [ inject the frontend mode flag    ] → first Navigate
```

Do it **between `Embed` and the first `Navigate`** and `app-region` is live on the
first page load. Do it *after* the first navigation and you must navigate a second time
for it to apply — that is exactly the "the app reloads once and flashes at startup"
pattern people mistake for a rendering bug. The same holds for any script you inject to
tell the frontend which title bar mode it is in: register it before the first
navigation, or the first paint uses the wrong mode.

**Order matters inside that window, too.** Enable *first*, and inject the "custom
title bar is on" flag *only if the enable succeeded*. If you inject the flag
unconditionally, then on a pre-131 runtime the frontend renders its custom title bar
while `app-region` is dead — and because the custom bar also replaces the classic
JS/bridge drag path, the window becomes **completely undraggable**. Failure of the
enable call must fall back to the classic title bar, whole.

---

## 6. What actually gives you Snap

Attempts made and their outcomes:

- **Custom `HTMAXBUTTON` from the parent's `WM_NCHITTEST`, on a client-extended
  (frameless) window.** The hit-test *is* reached — traces show `HTMAXBUTTON` being
  returned — and `DwmDefWindowProc` reports it as unhandled. **No flyout appears.**
  Returning the right code is necessary but not sufficient: once the client area is
  extended over the frame, DWM does not open the Snap popup for a synthetic maximize
  button.
- **Region-hole (`SetWindowRgn` to punch a hole in the child over the maximize
  button).** The hole does route the hit-test to the parent — `SetWindowRgn` works at
  the OS level, unlike `HTTRANSPARENT`, which is unreliable across process boundaries.
  But the hole is a hole: the WebView cannot paint those pixels, so the parent must,
  and the alignment has to be maintained across every resize and DPI change. And it
  still lands in the case above — trace, no flyout.
- **Layered click-through overlay window** drawn on top of the WebView. Works
  visually, cannot follow the DWM maximize animation (the overlay visibly lags/jumps),
  and is permanently fragile.

**The only configuration that produces the Snap hover flyout is a real, non-extended,
native caption band.** As soon as you extend the client area to swallow the caption,
the flyout is gone — no matter what your hit-test returns.

So: **"tabs are the title bar, the title bar is entirely HTML, and hovering my HTML
maximize button opens the Snap flyout" is infeasible in WebView2.** Do not spend weeks
on it. It is not a niche limitation either — **VS Code**, **Discord** and other shipping
Electron/WebView2 apps with custom title bars do not show the flyout on maximize hover.

State the consequence plainly in your own docs: **Snap itself still works.** `Win+Z`
(Snap Layouts), edge-drag (Aero Snap) and `Win`+arrow are unaffected — they are
shell/DWM features that do not depend on your hit-test. Only the *hover flyout on the
maximize button* is lost. That is a real cost, and for most applications it is the
right trade for a seamless HTML title bar.

**Two frameless side-quests you will hit anyway.** When you eat the caption in
`WM_NCCALCSIZE` (`wParam != 0`, return a client rect that covers the frame): a
maximized window will happily cover the taskbar unless you clamp the proposed client
rect to the monitor's **work area**; and restored windows typically need a one-pixel
compensation at the bottom edge, or the frame reads as clipped.

---

## 7. Decision table

| # | Approach | Snap flyout preserved? | HTML frontend kept? | Cost | Runtime requirement |
|---|---|:---:|:---:|---|---|
| 1 | **Non-client region** (`IsNonClientRegionSupportEnabled` + `app-region`) | **No** — caption only; drag / sysmenu / double-click, no hover flyout | Yes, fully | Medium — hand-bound COM + HTML bar | 131.0.2903.40+ |
| 2 | Window Controls Overlay (WCO) | Partially — WebView2 draws native buttons; used *with* #1, not instead | Yes | Medium-high | Newer runtime; pre-release API |
| 3 | Region-hole (`SetWindowRgn` hole in the child) | **No** — hit-test traces, flyout never opens on a client-extended window | Yes | High — hole alignment, parent paints those pixels | — |
| 4 | Chromium/Terminal-style custom frame (`DwmExtendFrameIntoClientArea` + `WM_NCCALCSIZE` + draw the frame yourself) | Yes (only if *you* draw the bar) | **No** — a WebView2 child re-masks the hover | Very high | — |
| 5 | DWM caption theming (recolour the native caption) | **Yes** — the native caption stays | Yes | **Low** | Windows 11 |
| 6 | Layered click-through overlay | Yes | Yes | Low but permanently fragile | — |
| 7 | WinUI 3 / Windows App SDK (platform draws the caption) | Yes | **No** — full UI rewrite | Very high | Windows App SDK |
| 8 | Different GUI toolkit (immediate-mode / retained Go toolkits, other frameless hosts) | Partial / undocumented | **No** — full rewrite | Very high | — |
| 9 | CGo + XAML Islands (`NonClientIslandWindow` pattern) | Yes — real maximize button, real `HTMAXBUTTON` | Partial — XAML bar + WebView2 content coexisting | **Highest** — C++/WinRT shim, SDK dependency, fragile | Windows App SDK 1.4+ |

Practical reading of the table: **#1 for the frontend, #5 if you want to keep the
flyout, #2 when it goes GA.** #3, #4, #6 and #9 are ways to spend a month and arrive
back at #1.

---

## 8. DWM caption theming (recolour the caption without breaking Snap)

If the flyout matters more than a seamless bar, keep the native caption and just make
it match your application. This is the cheapest option in the whole table and nothing
about Snap changes, because nothing about the non-client area changes.

`DwmSetWindowAttribute` (Windows 11, build 22000+):

| Attribute | Value | Effect |
|---|---|---|
| `DWMWA_BORDER_COLOR` | 34 | Window border colour |
| `DWMWA_CAPTION_COLOR` | 35 | Caption background colour |
| `DWMWA_TEXT_COLOR` | 36 | Caption text colour |
| `DWMWA_USE_IMMERSIVE_DARK_MODE` | 20 | Dark caption; **redundant once you set the colours explicitly** |

All three colour attributes take a `COLORREF`, which is `0x00BBGGRR` — **blue in the
high byte, red in the low byte, and the top byte must be zero.** Passing an RGB value
straight through is the classic bug: you get a plausible-looking wrong colour and
assume the API is broken. The value is passed by address with `sizeof(uint32)`.

**Removing the caption icon** (the small app icon at the left of the title bar, which
has no place in a themed bar):

1. `SendMessage(hwnd, WM_SETICON, ICON_SMALL /*0*/, 0)` and again with
   `ICON_BIG /*1*/`.
2. Add `WS_EX_DLGMODALFRAME` to the extended style — without it, Windows redraws the
   default icon.
3. `SetWindowPos(..., SWP_FRAMECHANGED | SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE)`
   to make the frame change take effect.

What this approach cannot give you: a centred title, a custom caption height, or a bar
that is continuous with your content. Those are exactly the things you gave up Snap for
in §6. Choose one.

---

## 9. The system menu with a custom title bar

Two independent things go wrong here, and they are easy to confuse with each other.

**(a) The menu does not appear at all.** Right-clicking an `app-region: drag` region
raises the system menu only if the window still has a system menu. If your window-style
profile strips `WS_SYSMENU` (a very common thing to do when going frameless, since it
also removes the caption buttons), `GetSystemMenu` has nothing to show and the
right-click silently does nothing. **Keep `WS_SYSMENU` in the style and remove the
caption visually via `WM_NCCALCSIZE` instead.** The same reasoning applies to
`WS_MAXIMIZEBOX`/`WS_MINIMIZEBOX`/`WS_THICKFRAME`: those bits also gate the *enabled
state* of the corresponding menu items, and standard `WM_SYSCOMMAND` handling depends
on them.

**(b) The menu appears with stale item states.** This one is genuinely surprising:

> **`DefWindowProc` does not update system-menu item states in `WM_INITMENU`.**

Probing a live window shows all six standard items still enabled while the window is
restored, and — worse — some real caption interactions (a maximize) write the
maximized-state item set into the menu and *never write it back* on restore. So a
restored window can show a menu with "Restore" enabled and "Move"/"Size"/"Maximize"
greyed out. Everything about the window's actual state (`IsZoomed`, `WS_MAXIMIZE`) is
correct; only the menu lies.

The fix is to force the state yourself, whichever path shows the menu — WebView2's
caption right-click, or your own `TrackPopupMenu` call from a custom title-bar button:

```
menu := GetSystemMenu(hwnd, FALSE)
for command, enabled := range desiredStates(...) {
    flags := MF_BYCOMMAND
    if !enabled { flags |= MF_GRAYED }
    EnableMenuItem(menu, command, flags)   // returns 0xFFFFFFFF if the item is absent
}
```

Standard commands: `SC_SIZE` 0xF000, `SC_MOVE` 0xF010, `SC_MINIMIZE` 0xF020,
`SC_MAXIMIZE` 0xF030, `SC_CLOSE` 0xF060, `SC_RESTORE` 0xF120. `MF_BYCOMMAND` is 0,
`MF_GRAYED` is 1.

The correct state matrix, derived from the window's *actual* state:

| Item | Restored | Maximized or minimized |
|---|---|---|
| Restore | greyed | **enabled** |
| Move | enabled | greyed |
| Size | enabled *iff* `WS_THICKFRAME` | greyed |
| Minimize | enabled *iff* `WS_MINIMIZEBOX` | enabled *iff* `WS_MINIMIZEBOX` |
| Maximize | enabled *iff* `WS_MAXIMIZEBOX` | greyed |
| Close | enabled | enabled |

Sync it in **two** places: on `WM_INITMENU` (the correctness backstop, right before the
menu is drawn) and eagerly on every `WM_SIZE` state transition (which also covers
double-click, edge-drag and `Win+Z`, i.e. the DWM-driven paths that never go through
your buttons). `WM_SIZE` fires at mouse-move rate during an interactive resize, so
cache the last synced `(zoomed, iconic, style)` triple and return early when nothing
changed.

If you raise the menu yourself, `TrackPopupMenu` with `TPM_RETURNCMD | TPM_RIGHTBUTTON`
gives you back the command id; forward it with
`PostMessage(hwnd, WM_SYSCOMMAND, cmd, 0)`. Call `SetForegroundWindow` on your window
first, or the menu can fail to dismiss on the next outside click.

---

## 10. Sources

The full source list — 40 links, grouped by topic and marked `[P]` primary /
`[F]` secondary — lives in [snap-sources.md](./snap-sources.md).
