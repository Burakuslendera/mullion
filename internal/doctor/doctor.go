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

	// Homes are the spellings of the current user's profile directory. They are
	// never printed: they are what paths are redacted against, so a runtime
	// pinned somewhere under the home directory does not carry a user name into
	// a public issue.
	//
	// Plural, because Windows hands out two. A profile directory whose name
	// contains a space also has an 8.3 short name, and a path that arrives in
	// that spelling sails straight past a redaction that only knows the long one
	// - carrying the first six characters of the user name with it. That is not
	// hypothetical: it is what the first live run of this command printed.
	Homes []string
}

// WebView2Section is the runtime mullion would actually load on this machine,
// and whether it can be driven at all.
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

// Monitor is one display, measured with per-monitor DPI awareness declared, so
// the numbers are physical rather than the virtualised ones Windows hands to a
// process that has not asked.
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

// Usable reports whether mullion can start on this machine: a runtime was
// selected, and it exports the entry point the host calls. It is what the
// command's exit code says, so that the report can be read by a script and not
// only by a person.
func (r Report) Usable() bool {
	return r.WebView2.Found && r.WebView2.ExportFound
}

// Format renders the report as the block a reporter pastes into an issue.
func Format(r Report) string {
	var out strings.Builder

	out.WriteString("```\n")
	build := fallback(r.Mullion, "unknown")
	field(&out, "mullion", build)
	if !identifiesTheBuild(build) {
		// The version line exists to name the code that was running. When it
		// cannot, saying so - and saying how to fix it - beats printing a word
		// that looks like an answer. "go run" stamps no VCS information; only
		// "go build", "go install" and an explicit -buildvcs=true do.
		note(&out, `no commit recorded - "go run" does not stamp it; use "go run -buildvcs=true" from a checkout`)
	}
	field(&out, "OS", fallback(r.OS, "unknown"))
	field(&out, "Arch", fallback(r.Arch, "unknown"))
	field(&out, "Go", fallback(r.Go, "unknown"))

	out.WriteString("\n")
	formatWebView2(&out, r.WebView2, r.Homes)

	if len(r.GPUs) > 0 {
		out.WriteString("\n")
		for _, gpu := range r.GPUs {
			field(&out, "GPU", gpu)
		}
	}

	if len(r.Monitors) > 0 {
		out.WriteString("\n")
		field(&out, "Monitors", fmt.Sprintf("%d", len(r.Monitors)))
		for index, monitor := range r.Monitors {
			primary := ""
			if monitor.Primary {
				primary = ", primary"
			}
			fmt.Fprintf(&out, "  [%d] %dx%d at %d%% (dpi %d), origin %d,%d, work area %dx%d%s\n",
				index+1, monitor.Width, monitor.Height, monitor.Scale(), monitor.DPI,
				monitor.Left, monitor.Top, monitor.WorkWidth, monitor.WorkHeight, primary)
		}
	}
	out.WriteString("```\n")

	if len(r.Monitors) > 0 {
		out.WriteString("\nMeasured with per-monitor DPI awareness, so the resolutions above are physical.\n")
	}
	return out.String()
}

func formatWebView2(out *strings.Builder, section WebView2Section, homes []string) {
	if !section.Found {
		field(out, "WebView2", "none usable")
		note(out, fallback(section.Problem, "no runtime was found"))
		if section.PinnedEnv != "" {
			note(out, "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER is set to "+redactHome(section.PinnedEnv, homes))
		}
		return
	}

	kind := "Evergreen"
	if section.Fixed {
		kind = "fixed-version"
	}
	field(out, "WebView2", fallback(section.Version, "unknown version")+" ("+kind+")")
	if section.Source != "" {
		note(out, "found via "+section.Source)
	}
	if section.Folder != "" {
		note(out, "folder "+redactHome(section.Folder, homes))
	}

	if section.ExportName == "" {
		return
	}
	if section.ExportFound {
		note(out, "exports "+section.ExportName+": yes")
		return
	}
	// The loud case. This is the failure the README's known limitation
	// describes, and the whole reason the command loads the DLL rather than
	// reading a version out of the registry and calling it a diagnosis.
	note(out, "exports "+section.ExportName+": NO")
	if section.ExportProblem != "" {
		note(out, section.ExportProblem)
	}
	note(out, "mullion cannot start against this runtime; see Known limitations in the README")
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

func field(out *strings.Builder, label, value string) {
	fmt.Fprintf(out, "%-11s%s\n", label+":", value)
}

// note is a continuation line under the field above it.
func note(out *strings.Builder, value string) {
	fmt.Fprintf(out, "%-11s%s\n", "", value)
}

func fallback(value, whenEmpty string) string {
	if strings.TrimSpace(value) == "" {
		return whenEmpty
	}
	return value
}

// redactHome replaces the user's profile directory with %USERPROFILE%, in any
// of the spellings Windows uses for it.
//
// The runtime folder has to be printed - "which binary was actually loaded" is
// the first question of any WebView2 failure - but a fixed-version runtime is
// often pinned somewhere under the home directory, and the block this command
// prints is written to be pasted into a public issue. The path keeps its
// meaning; the user name does not survive it.
func redactHome(path string, homes []string) string {
	for _, home := range homes {
		if redacted, ok := withoutHome(path, home); ok {
			return redacted
		}
	}
	return path
}

func withoutHome(path, home string) (string, bool) {
	home = strings.TrimRight(home, `\/`)
	if path == "" || home == "" {
		return path, false
	}
	if len(path) < len(home) || !strings.EqualFold(path[:len(home)], home) {
		return path, false
	}
	rest := path[len(home):]
	if rest != "" && rest[0] != '\\' && rest[0] != '/' {
		// A sibling folder whose name merely begins with the home directory's
		// name is not inside it, and must not be rewritten as though it were.
		return path, false
	}
	return "%USERPROFILE%" + rest, true
}
