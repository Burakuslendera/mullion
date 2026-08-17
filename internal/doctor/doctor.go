// Package doctor collects the environment a mullion bug report needs.
//
// It exists because the environment is half of every frame or DPI report, and
// the person filing one usually has no checkout of this repository - they have
// the library and a Go toolchain, which is all `mullion doctor` requires.
//
// The report is a plain struct and the formatting is a pure function, so the
// half that can be tested is tested headlessly (docs/decisions/0006). The half
// that has to touch Win32 is a thin probe, in probe_windows.go.
package doctor

import (
	"fmt"
	"strings"
)

// Report is everything `mullion doctor` prints.
type Report struct {
	Mullion  string
	OS       string
	Arch     string
	Go       string
	WebView2 WebView2Section
	GPUs     []string
	Monitors []Monitor

	// Homes contains up to two spellings of the current user's profile
	// directory: the long form when available and a distinct 8.3 short form
	// when Windows provides one. They are never printed; they are what paths are
	// redacted against, so either spelling cannot carry a user name into a
	// public issue.
	Homes []string
}

// WebView2Section describes the runtime mullion would select on this machine
// and whether it exposes the entry point the host requires.
type WebView2Section struct {
	// Found is true when a runtime was selected. Problem says why not, when not.
	Found   bool
	Problem string

	Version string
	Folder  string
	Source  string
	Fixed   bool

	// PinnedEnv is the value of WEBVIEW2_BROWSER_EXECUTABLE_FOLDER, empty when
	// it is not set. A report taken against a pinned runtime is a different
	// report, and the reader has to be told without having to ask.
	PinnedEnv string

	// ExportName is the entry point mullion calls; ExportFound says whether the
	// selected runtime really exports it. This is the one line in the whole
	// report that a registry lookup cannot produce.
	ExportName    string
	ExportFound   bool
	ExportProblem string
}

// Monitor is one readable entry in the best-effort display inventory. Its
// dimensions are physical when Windows accepts the preceding per-monitor-v2
// awareness request; the probe cannot guarantee that process-wide request.
type Monitor struct {
	Width, Height         int
	Left, Top             int
	WorkWidth, WorkHeight int
	DPI                   int
	Primary               bool
}

// Scale is the percentage Windows shows in its display settings.
func (m Monitor) Scale() int {
	if m.DPI <= 0 {
		return 100
	}
	// Integer rounding, so no floating point sneaks into a number a human will
	// compare against a settings panel.
	return (m.DPI*100 + 48) / 96
}

// Usable reports whether the prerequisites checked by doctor passed: a runtime
// was selected and it exports the entry point the host calls. This drives the
// command's exit code; it does not start a Host or prove hosting/rendering.
func (r Report) Usable() bool {
	return r.WebView2.Found && r.WebView2.ExportFound
}

// Format renders the report as the block a reporter pastes into an issue.
func Format(r Report) string {
	writer := publicReportWriter{homes: r.Homes}

	writer.out.WriteString("```\n")
	build := fallback(r.Mullion, "unknown")
	writer.field("mullion", build)
	if !identifiesTheBuild(build) {
		// The version line exists to name the code that was running. When it
		// cannot, saying so - and saying how to diagnose it - beats printing a
		// word that looks like an answer. Go's default -buildvcs=auto stamps only
		// when its repository and tool conditions are met.
		writer.note(`no commit recorded in a usable VCS stamp; retry with "go run -buildvcs=true" from a checkout to require and diagnose stamping`)
	}
	writer.field("OS", fallback(r.OS, "unknown"))
	writer.field("Arch", fallback(r.Arch, "unknown"))
	writer.field("Go", fallback(r.Go, "unknown"))

	writer.out.WriteString("\n")
	formatWebView2(&writer, r.WebView2)

	if len(r.GPUs) > 0 {
		writer.out.WriteString("\n")
		for _, gpu := range r.GPUs {
			writer.field("GPU", gpu)
		}
	}

	if len(r.Monitors) > 0 {
		writer.out.WriteString("\n")
		writer.field("Monitors", fmt.Sprintf("%d", len(r.Monitors)))
		for index, monitor := range r.Monitors {
			primary := ""
			if monitor.Primary {
				primary = ", primary"
			}
			fmt.Fprintf(&writer.out, "  [%d] %dx%d at %d%% (dpi %d), origin %d,%d, work area %dx%d%s\n",
				index+1, monitor.Width, monitor.Height, monitor.Scale(), monitor.DPI,
				monitor.Left, monitor.Top, monitor.WorkWidth, monitor.WorkHeight, primary)
		}
	}
	writer.out.WriteString("```\n")

	if len(r.Monitors) > 0 {
		writer.out.WriteString("\nMonitor inventory is best effort after requesting per-monitor DPI awareness; reported resolutions are physical when Windows accepted that request.\n")
	}
	return writer.out.String()
}

func formatWebView2(writer *publicReportWriter, section WebView2Section) {
	if !section.Found {
		writer.field("WebView2", "none usable")
		writer.note(fallback(section.Problem, "no runtime was found"))
		if section.PinnedEnv != "" {
			writer.note("WEBVIEW2_BROWSER_EXECUTABLE_FOLDER is set to " + section.PinnedEnv)
		}
		return
	}

	kind := "Evergreen"
	if section.Fixed {
		kind = "fixed-version"
	}
	writer.field("WebView2", fallback(section.Version, "unknown version")+" ("+kind+")")
	if section.Source != "" {
		writer.note("found via " + section.Source)
	}
	if section.Folder != "" {
		writer.note("folder " + section.Folder)
	}

	if section.ExportName == "" {
		return
	}
	if section.ExportFound {
		writer.note("exports " + section.ExportName + ": yes")
		return
	}
	// The loud case. This is the failure the README's known limitation
	// describes, and the whole reason the command loads the DLL rather than
	// reading a version out of the registry and calling it a diagnosis.
	writer.note("exports " + section.ExportName + ": NO")
	if section.ExportProblem != "" {
		writer.note(section.ExportProblem)
	}
	writer.note("mullion cannot start against this runtime; see Known limitations in the README")
}

// identifiesTheBuild reports whether the version line names the code that was
// running. "devel" with no revision behind it does not, and neither does
// "unknown": both are the version's own way of admitting it does not know.
func identifiesTheBuild(version string) bool {
	switch strings.TrimSpace(version) {
	case "devel", "unknown", "":
		return false
	default:
		return true
	}
}

func fallback(value, whenEmpty string) string {
	if strings.TrimSpace(value) == "" {
		return whenEmpty
	}
	return value
}
