module github.com/Burakuslendera/mullion

go 1.22

// The only dependency. Everything WebView2-related - locating the runtime,
// creating the environment, the COM interfaces, the event handlers - is
// implemented in internal/webview2 against Microsoft's published interface
// definitions, so there is no third-party browser binding to keep in step with.
require golang.org/x/sys v0.27.0
