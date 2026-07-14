package mullion

import (
	"runtime/debug"
	"strings"
)

const modulePath = "github.com/Burakuslendera/mullion"

// Version reports which build of mullion is linked into the running program.
//
// It is not a constant that somebody has to remember to bump. Go records the
// version of every module in the binary itself, so this reads the truth rather
// than a promise:
//
//   - a tagged release reads as "v0.1.0";
//   - an untagged commit reads as its pseudo-version,
//     "v0.0.0-20060102150405-abcdef123456", which carries the commit hash;
//   - a local replace directive says so, because a bug report from a patched
//     copy is a different bug report;
//   - a build of mullion itself reads as "devel" plus the revision, and says
//     when the working tree was dirty.
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

	// Case 1: mullion is the main module. Somebody is working on the library, or
	// running its example. The module version is meaningless here ("(devel)"), so
	// report the revision instead - that is what identifies the build.
	if info.Main.Path == modulePath {
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
	return "mullion: version=" + Version() +
		", go=" + goVersion +
		", arch=" + arch +
		", webview2=" + strings.TrimSpace(webViewVersion)
}
