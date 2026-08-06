# What a good bug report contains

Section 6 of [verification.md](./verification.md) - how a change to `mullion` is
proved correct - moved here verbatim when that file reached the 400-line
reference-doc limit. Nothing below was rewritten in the move.

Frame bugs are environment-dependent; a report without environment is a report
that cannot be reproduced.

**Do not gather the environment by hand. Run it:**

```
go run github.com/Burakuslendera/mullion/cmd/mullion@latest doctor   # no checkout needed
go run -buildvcs=true ./cmd/mullion doctor                           # from a checkout
go install ./cmd/mullion                                             # keep it: $(go env GOPATH)/bin
```

It prints a paste-ready block: Windows build (corrected — the registry still says
"Windows 10" on Windows 11), process architecture, GPUs, every monitor with its
**physical** resolution, scaling and work area, and the WebView2 runtime — not
the one the registry advertises, but **the one mullion would actually load**,
together with whether it still exports the entry point the host calls. Exit code
0 means mullion can start on that process architecture. Unsupported Windows/386
and Windows/ARM64 targets exit 1 with the reason before reading or expanding a
pinned runtime path and before DPI, registry, GPU, home-directory or monitor
probes. See [decision 0034](./decisions/0034-webview2-hosting-is-windows-amd64-only.md).

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
- **The asset source** — whether `Config.URL` pointed the WebView at a caller-served
  loopback origin instead of the embedded `fs.FS`. The startup
  `mullion: asset source=` line records it. With a caller URL the served bytes and
  the asset boundary are the caller's, not mullion's, so it is a different report.
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

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: consolidate accumulated edit signatures into the single current footer required by agents/notes.md; Git retains the earlier history.
