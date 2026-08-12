# Snap Testing Boundary

## 10. Testing boundary

The Settings9 IID/vtable layout, `BOOL` calling convention, and startup ordering
are deterministic headless contracts. They prevent an ABI crash or an
old-runtime fallback regression, but they cannot prove compositor hover
behaviour: `HTMAXBUTTON` delivery through the WebView child, the Snap flyout,
caption drag, system-menu presentation, and DPI-aligned region geometry all
require a live Windows 11 session with the target WebView2 Runtime. Do not
promote a headless binding or routing test into evidence that the flyout works;
test the actual chosen frame profile on the desktops and monitor scales it
supports.

> Last updated: 2026-08-12 | Editor: OpenAI (GPT-5.6) | Change: extract the headless-versus-live Snap verification boundary from the main Snap guide.
