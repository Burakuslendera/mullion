# WebView2 Zoom and Native Hit Testing

## 11. Chromium zoom must be off

On the WebView2 settings object — note that the two live on *different* interfaces
in the settings chain, so pinch zoom is reached through a `QueryInterface` for
`ICoreWebView2Settings5` and is a no-op on a runtime that does not offer it:

```go
settings.PutIsZoomControlEnabled(false)   // ICoreWebView2Settings
settings5.PutIsPinchZoomEnabled(false)    // ICoreWebView2Settings5
```

- **Symptom:** after an accidental `Ctrl+scroll`, the title bar no longer lines up
  with the drag region, the caption buttons are near but not under the cursor, and
  the resize band is off by a few pixels.
- **Root cause:** user zoom rescales the CSS layer only. The native hit-test
  regions (`TitlebarHeight`, `CaptionControlsWidth`, `ResizeBorder`) are computed
  from logical px and DPI and know nothing about it. The two coordinate systems
  drift apart, and no event lets the native side follow along reliably.
- **Fix:** disable zoom control and pinch zoom at controller setup. If you need a
  zoom feature, scale the UI with your own CSS variables — a scale factor you own
  can be mirrored into `HitTestTitlebarHeight` / `HitTestCaptionControlsWidth`;
  Chromium's cannot.

> Last updated: 2026-08-12 | Editor: OpenAI (GPT-5.6) | Change: extract the WebView2 zoom and native hit-testing contract from the frame guide.
