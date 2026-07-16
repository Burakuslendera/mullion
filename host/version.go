package host

import (
	"runtime/debug"
	"strings"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

const modulePath = "github.com/Burakuslendera/mullion"

// Version reports which build of mullion is linked into the running program.
//
// It is not a constant that somebody has to remember to bump. Go records the
// version of every module in the binary itself, so this reads the truth rather
// than a promise:
//
//   - a tagged release reads as "v0.0.1";
//   - an untagged commit reads as its pseudo-version,
//     "v0.0.0-20060102150405-abcdef123456", which carries the commit hash;
//   - a local replace directive says so, because a bug report from a patched
//     copy is a different bug report;
//   - a build of mullion itself reads as "devel" plus the revision, and says
//     when the working tree was dirty. Note that "go run" does not stamp the
//     revision at all - only "go build", "go install", or an explicit
//     "go run -buildvcs=true" do - so a build with nothing to report reads as a
//     bare "devel" rather than pretending to know.
//
// Run logs this line at startup, so a report that includes the log already
// answers "which version" without anyone having to ask.
func Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		// Only happens for a binary built without module information, which is
		// rare enough that guessing would be worse than admitting it.
		return "unknown"
	}
	return versionFrom(info)
}

// versionFrom is separated from Version so it can be tested against a fabricated
// build info: the real one cannot be forged inside a test binary.
func versionFrom(info *debug.BuildInfo) string {
	if info == nil {
		return "unknown"
	}

	// Case 1: mullion is the main module. Two very different builds land here and
	// the VCS stamp is what tells them apart.
	//
	// A build from a working tree carries one. Since Go 1.24 it *also* carries a
	// synthesized version in Main.Version - "v0.0.0-<time>-<revision>", with
	// "+dirty" appended - which reads exactly like a released pseudo-version and
	// is not one. Passing it on would tell a reporter they are running a release.
	// The revision, and whether the tree was dirty, remain the answer.
	//
	// A module fetched from the proxy carries no stamp. That is "go run
	// github.com/.../cmd/mullion@v0.1.0": the version is real, it is the answer,
	// and there is no revision to report at all.
	if info.Main.Path == modulePath {
		if hasVCSStamp(info) {
			return develVersion(info)
		}
		if version := releasedVersion(info.Main.Version); version != "" {
			return version
		}
		return develVersion(info)
	}

	// Case 2: mullion is a dependency, which is the shipping case.
	for _, dep := range info.Deps {
		if dep == nil || dep.Path != modulePath {
			continue
		}
		if dep.Replace != nil {
			replacement := dep.Replace.Version
			if replacement == "" {
				replacement = dep.Replace.Path
			}
			return dep.Version + " (replaced by " + replacement + ")"
		}
		return dep.Version
	}

	return "unknown"
}

// releasedVersion returns a module version only when it names a release.
// Older toolchains write "(devel)" for a build from a checkout, which
// identifies nothing.
func releasedVersion(version string) string {
	switch strings.TrimSpace(version) {
	case "", "devel", "(devel)":
		return ""
	default:
		return strings.TrimSpace(version)
	}
}

// hasVCSStamp reports whether this binary was built from a working tree. It is
// the one signal that separates a local build from a module fetched by version,
// and it does not depend on the shape of Main.Version - which the toolchain has
// already changed once.
func hasVCSStamp(info *debug.BuildInfo) bool {
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			return true
		}
	}
	return false
}

func develVersion(info *debug.BuildInfo) string {
	revision := ""
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}

	if revision == "" {
		return "devel"
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	version := "devel (" + revision + ")"
	if modified {
		// A dirty tree means the revision names a commit the running code is not.
		// Saying so is the difference between a reproducible report and a false one.
		version = "devel (" + revision + ", modified)"
	}
	return version
}

// runtimeSummary is the one line Run logs at startup. It exists so that a pasted
// log answers the first three questions of any bug report - which build, on what
// architecture, against which browser runtime - without a round trip.
func runtimeSummary(webViewVersion string, goVersion string, arch string) string {
	if webViewVersion == "" {
		webViewVersion = "unknown"
	}
	// webViewVersion can originate in an unprivileged HKCU registry value, so it
	// is sanitised (internal/webview2.sanitizeVersion) at the source; logsafe here
	// is defence in depth for any other origin before the line reaches a Logger
	// that may render it in a terminal.
	return "mullion: version=" + Version() +
		", go=" + goVersion +
		", arch=" + arch +
		", webview2=" + logsafe.Message(webViewVersion)
}
