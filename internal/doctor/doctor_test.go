package doctor

import (
	"strings"
	"testing"
)

// The report is a contract with two readers who never meet: the person pasting
// it into an issue, and the person triaging it a week later. What is locked here
// is what the second one must never have to ask for.

func healthy() Report {
	return Report{
		Mullion: "v0.1.0",
		OS:      "Windows 11 Pro 24H2 (build 26100.1234)",
		Arch:    "amd64",
		Go:      "go1.22.5",
		WebView2: WebView2Section{
			Found:       true,
			Version:     "150.0.4078.65",
			Folder:      `C:\Program Files (x86)\Microsoft\EdgeWebView\Application\150.0.4078.65`,
			Source:      "HKLM EdgeUpdate (32-bit view)",
			ExportName:  "CreateWebViewEnvironmentWithOptionsInternal",
			ExportFound: true,
		},
		GPUs: []string{"Example Graphics Adapter (driver 1.2.3.4)"},
		Monitors: []Monitor{
			{Width: 1920, Height: 1080, WorkWidth: 1920, WorkHeight: 1032, DPI: 96, Primary: true},
			{Width: 2560, Height: 1440, Left: 1920, WorkWidth: 2560, WorkHeight: 1392, DPI: 144},
		},
	}
}

func TestFormatCarriesWhatABugReportNeeds(t *testing.T) {
	report := healthy()
	if !report.Usable() {
		t.Fatal("a runtime that exports the entry point must be reported as usable")
	}

	out := Format(report)
	for _, want := range []string{
		"v0.1.0",
		"build 26100.1234",
		"150.0.4078.65 (Evergreen)",
		"HKLM EdgeUpdate (32-bit view)",
		"exports CreateWebViewEnvironmentWithOptionsInternal: yes",
		"Example Graphics Adapter",
		"[1] 1920x1080 at 100% (dpi 96), origin 0,0, work area 1920x1032, primary",
		"[2] 2560x1440 at 150% (dpi 144), origin 1920,0, work area 2560x1392",
		"per-monitor DPI awareness",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not carry %q:\n%s", want, out)
		}
	}
	// The second monitor is not primary and must not claim to be.
	if strings.Count(out, "primary") != 1 {
		t.Errorf("exactly one monitor may be marked primary:\n%s", out)
	}
}

// The loud case. A runtime that is installed but no longer exports the entry
// point is the known limitation in the README firing, and it is the one thing
// this command exists to catch. Reporting it quietly - or worse, reporting the
// version and stopping - would make the tool an accomplice.
func TestFormatSaysSoWhenTheExportIsGone(t *testing.T) {
	report := healthy()
	report.WebView2.ExportFound = false
	report.WebView2.ExportProblem = "webview2: EmbeddedBrowserWebView.dll does not export CreateWebViewEnvironmentWithOptionsInternal"

	if report.Usable() {
		t.Fatal("a runtime that cannot be driven must not be reported as usable")
	}

	out := Format(report)
	for _, want := range []string{
		"exports CreateWebViewEnvironmentWithOptionsInternal: NO",
		"does not export",
		"mullion cannot start against this runtime",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not carry %q:\n%s", want, out)
		}
	}
}

func TestFormatSaysSoWhenThereIsNoRuntime(t *testing.T) {
	report := healthy()
	report.WebView2 = WebView2Section{
		Problem:    "webview2: no WebView2 runtime found; install the Evergreen WebView2 Runtime or set WEBVIEW2_BROWSER_EXECUTABLE_FOLDER",
		ExportName: "CreateWebViewEnvironmentWithOptionsInternal",
	}

	if report.Usable() {
		t.Fatal("no runtime means mullion cannot start; the report must say so")
	}
	out := Format(report)
	if !strings.Contains(out, "none usable") || !strings.Contains(out, "no WebView2 runtime found") {
		t.Errorf("the report does not explain the absence:\n%s", out)
	}
}

// A pinned runtime is a different report, and a reader who is not told will
// reproduce against the installed one and disagree with the reporter forever.
func TestFormatDisclosesAPinnedRuntime(t *testing.T) {
	report := healthy()
	report.WebView2.Fixed = true
	report.WebView2.Source = "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER"

	out := Format(report)
	if !strings.Contains(out, "(fixed-version)") {
		t.Errorf("a fixed-version runtime must be named as one:\n%s", out)
	}
}

func TestFormatOnAPlatformThatCannotRunTheWindow(t *testing.T) {
	report := Report{Mullion: "v0.1.0", OS: "linux", Arch: "arm64", Go: "go1.22.5"}
	report.WebView2.Problem = "mullion runs on Windows only; there is no WebView2 runtime to describe on linux"

	if report.Usable() {
		t.Fatal("a platform with no WebView2 cannot be reported as usable")
	}
	out := Format(report)
	if !strings.Contains(out, "Windows only") {
		t.Errorf("the report does not say why there is nothing to describe:\n%s", out)
	}
	// Nothing was measured, so nothing may be claimed about monitors.
	if strings.Contains(out, "per-monitor DPI awareness") {
		t.Errorf("the report claims a measurement it never made:\n%s", out)
	}
}

// The version line exists to name the code that was running. "go run" stamps no
// VCS information, so from a checkout it names nothing - and a report that
// quietly prints "devel" has spent its most important line saying nothing at
// all. It has to say that, and say how to fix it.
func TestFormatAdmitsWhenItCannotIdentifyTheBuild(t *testing.T) {
	report := healthy()
	report.Mullion = "devel"

	out := Format(report)
	if !strings.Contains(out, "no commit recorded") || !strings.Contains(out, "-buildvcs=true") {
		t.Errorf("a version line that identifies nothing must say so, and say what to run instead:\n%s", out)
	}

	// A build that does identify itself must not carry the hint.
	report.Mullion = "devel (abcdef1)"
	if strings.Contains(Format(report), "no commit recorded") {
		t.Error("a build with a revision behind it identifies the code; the hint is noise there")
	}
	report.Mullion = "v0.1.0"
	if strings.Contains(Format(report), "no commit recorded") {
		t.Error("a released build identifies the code; the hint is noise there")
	}
}

func TestMonitorScaleMatchesTheSettingsPanel(t *testing.T) {
	cases := map[int]int{96: 100, 120: 125, 144: 150, 168: 175, 192: 200, 0: 100}
	for dpi, want := range cases {
		if got := (Monitor{DPI: dpi}).Scale(); got != want {
			t.Errorf("Monitor{DPI: %d}.Scale() = %d, want %d", dpi, got, want)
		}
	}
}

// The block this command prints is written to be pasted into a public issue. A
// fixed-version runtime is often pinned under the home directory, so the path
// keeps its meaning and loses the user name. The names below are invented.
func TestRedactHomeKeepsThePathAndDropsTheUser(t *testing.T) {
	const long = `C:\Users\Example User`
	const short = `C:\Users\EXAMPL~1`
	homes := []string{long, short}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"under the home directory", long + `\rt\120.0.0.1`, `%USERPROFILE%\rt\120.0.0.1`},
		{"case is not a hiding place", `c:\users\example user\rt`, `%USERPROFILE%\rt`},
		// The one the live run found. Windows hands out the 8.3 name for a
		// profile directory with a space in it, and six characters of the user
		// name ride along inside it.
		{"the 8.3 short name is redacted too", short + `\rt`, `%USERPROFILE%\rt`},
		{"the home directory itself", long, `%USERPROFILE%`},
		{"a sibling with a longer name is not inside it", long + `2\rt`, long + `2\rt`},
		{"a path elsewhere is left alone", `D:\fixed\120.0.0.1`, `D:\fixed\120.0.0.1`},
		// A runtime on a network share is never under the home directory, so the
		// redaction above never sees it; the internal host name is collapsed.
		{"a UNC host is collapsed, keeping the share path", `\\BUILD-NAS\tools\webview2\120.0.0.1`, `\\<host>\tools\webview2\120.0.0.1`},
		{"a forward-slash UNC host is collapsed too", `//BUILD-NAS/tools/rt`, `//<host>/tools/rt`},
		{"a bare UNC host still loses its name", `\\BUILD-NAS`, `\\<host>`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := redactHome(testCase.path, homes); got != testCase.want {
				t.Fatalf("redactHome(%q) = %q, want %q", testCase.path, got, testCase.want)
			}
		})
	}

	// A trailing separator on the home directory must not defeat it.
	if got := redactHome(long+`\rt`, []string{long + `\`}); got != `%USERPROFILE%\rt` {
		t.Errorf("a trailing separator defeated the redaction: %q", got)
	}
	// Nothing known: the path is left exactly as it came.
	if got := redactHome(`D:\fixed`, nil); got != `D:\fixed` {
		t.Errorf("redactHome with no home directory = %q, want it untouched", got)
	}
}

// Format must never print a home directory it was given: they are the redaction
// key, not a field.
func TestFormatNeverPrintsTheHomeDirectory(t *testing.T) {
	report := healthy()
	report.Homes = []string{`C:\Users\Example User`, `C:\Users\EXAMPL~1`}
	report.WebView2.Folder = `C:\Users\EXAMPL~1\webview2\120.0.0.1`
	report.WebView2.PinnedEnv = `C:\Users\Example User\webview2\120.0.0.1`
	report.WebView2.Found = false
	report.WebView2.Problem = "pinned folder has no runtime"

	out := Format(report)
	for _, leak := range []string{"Example User", "EXAMPL~1"} {
		if strings.Contains(out, leak) {
			t.Fatalf("%q survived into a report written to be pasted in public:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, `%USERPROFILE%\webview2\120.0.0.1`) {
		t.Fatalf("the redacted path lost its meaning:\n%s", out)
	}
}

// Several report fields are read from the registry (ProductName, a GPU
// DriverDesc) or the environment (the pinned-folder path) and are not
// disk-verified, and Format's output is printed straight to a terminal. A
// smuggled ESC/OSC/BEL must be folded before it can rewrite the console title or
// erase provenance in front of the operator - the same class the loader strips
// from the "pv" version (issue #22), here from its sibling sources (issue #40).
func TestFormatFoldsControlBytesFromUntrustedFields(t *testing.T) {
	report := healthy()
	report.OS = "Windows 11 \x1b]0;pwned\x07 Pro"
	report.GPUs = []string{"Evil\x1b[2K Adapter (driver \x7f1.0)"}
	report.WebView2.Found = false
	report.WebView2.Problem = "pinned folder \x08has no runtime"
	report.WebView2.PinnedEnv = "D:\\pins\\rt\x85name"

	out := Format(report)
	for _, r := range out {
		// The only control character a formatted block may carry is the newline
		// between its lines; field and note add those after folding each value.
		if (r < 0x20 && r != '\n') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("a control byte %#x survived into the pasted block:\n%q", r, out)
		}
	}
	// Folding must not eat the readable text around the escape.
	for _, want := range []string{"Windows 11", "Adapter", "pwned"} {
		if !strings.Contains(out, want) {
			t.Errorf("folding dropped legible text %q:\n%s", want, out)
		}
	}
}
