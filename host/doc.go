// Package host is mullion's window host: it hosts a WebView2 control inside a
// Win32 window that the application owns end to end.
//
// The window is frameless: the title bar, the caption buttons, the resize
// borders, the system menu and the snap behaviour are all driven from the
// window procedure, so the frontend can render its own title bar without giving
// up the shell integration a native caption provides. Assets are served to the
// WebView from an fs.FS over an in-process virtual host, so no local port is
// opened and no HTTP server is started.
//
// The package is pure Go: it calls user32, dwmapi, kernel32 and shlwapi through
// syscall wrappers and needs no CGo toolchain.
//
// Windows only. On every other platform New returns a Host whose Run reports
// ErrUnsupportedPlatform, so a cross-platform program can compile and degrade
// rather than fail to build.
//
// A minimal host:
//
//	//go:embed all:frontend
//	var embedded embed.FS
//
//	func main() {
//		assets, err := fs.Sub(embedded, "frontend")
//		if err != nil {
//			log.Fatal(err)
//		}
//		host := host.New(host.Config{Assets: assets, Title: "Demo"})
//		if err := host.Run(); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// Run blocks, locks the calling goroutine to its OS thread and owns the message
// loop until the window closes. Every other Host method is safe to call from any
// goroutine: they post to the UI thread rather than touching the HWND directly.
//
// See docs/architecture.md for the bootstrap contract, docs/frame-and-dpi.md for
// the frame and DPI rules, and docs/snap-and-nonclient-region.md for what
// WebView2 can and cannot do with Windows 11 snap.
package host
