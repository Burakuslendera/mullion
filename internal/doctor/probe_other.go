//go:build !windows

package doctor

import "runtime"

// Probe answers honestly on a platform that cannot run a mullion window.
//
// The command still prints a report rather than refusing to run: a
// cross-platform consumer's build machine is a legitimate place to ask, and one
// line that says so is a better answer than a binary that will not start. The
// library itself takes the same position - it compiles everywhere and returns
// ErrUnsupportedPlatform at Run (docs/decisions/0007).
func Probe(version string) Report {
	return Report{
		Mullion: version,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Go:      runtime.Version(),
		WebView2: WebView2Section{
			Problem: "mullion runs on Windows only; there is no WebView2 runtime to describe on " + runtime.GOOS,
		},
	}
}
