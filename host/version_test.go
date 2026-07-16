package host

import (
	"runtime/debug"
	"strings"
	"testing"
)

// The build info of a test binary cannot be forged, so the pure function behind
// Version is tested instead. What is being locked here is a bug-report contract:
// the version line has to identify the exact code that was running, and it has to
// say so when it cannot.

func TestVersionFromTaggedDependency(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/app", Version: "v1.2.3"},
		Deps: []*debug.Module{
			{Path: "golang.org/x/sys", Version: "v0.27.0"},
			{Path: modulePath, Version: "v0.1.0"},
		},
	}
	if got := versionFrom(info); got != "v0.1.0" {
		t.Fatalf("versionFrom() = %q, want v0.1.0", got)
	}
}

// An untagged commit is reported by Go as a pseudo-version, and the commit hash
// is inside it. That is the whole answer to "which commit are you on" for a
// consumer who never cloned the repository.
func TestVersionFromUntaggedCommitCarriesTheHash(t *testing.T) {
	const pseudo = "v0.0.0-20060102150405-abcdef123456"
	info := &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/app"},
		Deps: []*debug.Module{{Path: modulePath, Version: pseudo}},
	}
	got := versionFrom(info)
	if got != pseudo {
		t.Fatalf("versionFrom() = %q, want %q", got, pseudo)
	}
	if !strings.Contains(got, "abcdef123456") {
		t.Fatalf("version %q does not carry the commit hash", got)
	}
}

// A replaced module is a different code base wearing the same version number. A
// report from one, read as if it came from the released build, wastes the time of
// everyone who tries to reproduce it.
func TestVersionFromReplacedDependencySaysSo(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/app"},
		Deps: []*debug.Module{{
			Path:    modulePath,
			Version: "v0.1.0",
			Replace: &debug.Module{Path: "../mullion"},
		}},
	}
	got := versionFrom(info)
	if !strings.Contains(got, "replaced by") {
		t.Fatalf("versionFrom() = %q, want it to disclose the replace directive", got)
	}
}

// fakeRevision is a full-length git revision, assembled rather than written out.
// A 40-character hex literal in the source is indistinguishable from a leaked
// build-artefact hash, and scripts/leak-scan.ps1 correctly refuses to tell them
// apart - so the fixture is built instead of pasted.
func fakeRevision() string {
	return "abcdef1" + strings.Repeat("0", 33)
}

// The command is run straight out of the module - "go run .../cmd/mullion@v0.1.0" -
// which makes mullion the main module *and* gives it a real version. A module
// fetched from the proxy carries no VCS stamp, so reading the revision would
// report "devel" for a tagged release: a version line that identifies nothing,
// in the one report that exists to identify the build.
func TestVersionFromInstalledCommandReportsItsRelease(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Path: modulePath, Version: "v0.1.0"},
	}
	if got := versionFrom(info); got != "v0.1.0" {
		t.Fatalf("versionFrom() = %q, want v0.1.0", got)
	}
}

// A build from a working tree. The fixture is what the toolchain actually
// produces, which is the point of it: since Go 1.24 the main module's version
// is a *synthesized* pseudo-version built from the VCS stamp, not the "(devel)"
// older toolchains wrote. It reads exactly like a released pseudo-version.
// Reporting it verbatim would tell a reporter they are running a release of
// mullion when they are running their own working tree - so the presence of the
// stamp, not the shape of the string, decides.
func TestVersionFromDevelBuildReportsRevisionNotTheSynthesizedVersion(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Path: modulePath, Version: "v0.0.0-20060102150405-abcdef123456"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: fakeRevision()},
			{Key: "vcs.modified", Value: "false"},
		},
	}
	if got := versionFrom(info); got != "devel (abcdef1)" {
		t.Fatalf("versionFrom() = %q, want devel (abcdef1)", got)
	}
}

// "go run" stamps no VCS information - only "go build", "go install" and an
// explicit "go run -buildvcs=true" do. There is then nothing to identify the
// build with, and the honest answer is to say so rather than to invent one.
func TestVersionFromAnUnstampedLocalBuildAdmitsIt(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "(devel)"}}
	if got := versionFrom(info); got != "devel" {
		t.Fatalf("versionFrom() = %q, want devel", got)
	}
}

func TestVersionFromDevelBuildReportsRevision(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Path: modulePath, Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: fakeRevision()},
			{Key: "vcs.modified", Value: "false"},
		},
	}
	if got := versionFrom(info); got != "devel (abcdef1)" {
		t.Fatalf("versionFrom() = %q, want devel (abcdef1)", got)
	}
}

// A dirty working tree means the revision names a commit that the running code is
// not. Silently reporting the commit anyway produces a report nobody can
// reproduce, and the reporter would never know why.
func TestVersionFromDirtyTreeSaysModified(t *testing.T) {
	info := &debug.BuildInfo{
		// The toolchain marks the synthesized version "+dirty" too. It is not
		// read: vcs.modified is the field that means it.
		Main: debug.Module{Path: modulePath, Version: "v0.0.0-20060102150405-abcdef123456+dirty"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: fakeRevision()},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	got := versionFrom(info)
	if !strings.Contains(got, "modified") {
		t.Fatalf("versionFrom() = %q, want it to disclose the dirty tree", got)
	}
}

func TestVersionFromUnknownIsAdmitted(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/app"},
		Deps: []*debug.Module{{Path: "golang.org/x/sys", Version: "v0.27.0"}},
	}
	if got := versionFrom(info); got != "unknown" {
		t.Fatalf("versionFrom() = %q, want unknown", got)
	}
	if got := versionFrom(nil); got != "unknown" {
		t.Fatalf("versionFrom(nil) = %q, want unknown", got)
	}
}

// The startup line is the one a reporter pastes without being asked. It has to
// carry the three facts that otherwise cost a round trip each.
func TestRuntimeSummaryCarriesTheBuildFacts(t *testing.T) {
	summary := runtimeSummary("150.0.4078.65", "go1.22.5", "amd64")
	for _, want := range []string{"mullion: version=", "go=go1.22.5", "arch=amd64", "webview2=150.0.4078.65"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q is missing %q", summary, want)
		}
	}
	if !strings.Contains(runtimeSummary("", "go1.22.5", "amd64"), "webview2=unknown") {
		t.Fatal("a missing runtime version must be admitted, not omitted")
	}
}

// TestRuntimeSummaryNeutralisesControlBytes locks the terminal-escape defence on
// the startup line. The WebView2 version can originate in an unprivileged HKCU
// registry value, so a control byte smuggled into it must not survive into the
// log line (and thence a terminal-rendering Logger); logsafe strips it.
func TestRuntimeSummaryNeutralisesControlBytes(t *testing.T) {
	summary := runtimeSummary("9999.0\x1b]0;pwned\x07\x1b[2K", "go1.22.5", "amd64")
	for _, r := range summary {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("summary still carries control byte %#x: %q", r, summary)
		}
	}
	if !strings.Contains(summary, "9999.0") {
		t.Fatalf("summary dropped the version digits: %q", summary)
	}
}
