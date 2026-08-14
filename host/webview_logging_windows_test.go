//go:build windows

package host

import (
	"strings"
	"testing"
)

// These lock the two WebView2 log lines that used to be unreachable from the
// suite. They were unreachable only because they sat inline in closures inside
// createWebView; lifting them into methods (webview_logging_windows.go) is the
// whole seam, and neither body touches the browser or any COM object.
//
// It matters that they are locked at all. Before issue #78 both deleted the host
// of every URL they printed, and both are lines a live verification is read
// from - so a silent regression here is a regression in the evidence, not just
// in a log.

// The navigation line must name where the navigation went. Through
// logsafe.Message it read "uri=httpindex.html?in=1": host deleted, query kept.
func TestLogNavigationStartingNamesTheHost(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})

	host.logNavigationStarting("https://mullion.localhost/index.html?in=1", 41, true, false)

	logged := logger.String()
	if !strings.Contains(logged, "uri=https://mullion.localhost/index.html?\n") {
		t.Fatalf("navigation line does not carry the host and query marker:\n%s", logged)
	}
	if strings.Contains(logged, "in=1") {
		t.Fatalf("navigation line leaked the query:\n%s", logged)
	}
	// The id ties this line to the completion that follows it.
	if !strings.Contains(logged, "id=41") {
		t.Fatalf("navigation line lost the id:\n%s", logged)
	}
}

// The rejection pair: the WARN names the origin the allow-list decided on, the
// DEBUG carries the fuller reduction. Both went through the mangler before.
func TestLogRejectedWebMessageNamesTheOrigin(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})

	host.logRejectedWebMessage("https://evil.example/x?token=s3cr3t")

	logged := logger.String()
	for _, want := range []string{
		"origin=https://evil.example\n",
		"raw source=https://evil.example/x?",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("rejection log does not carry %q:\n%s", want, logged)
		}
	}
	if strings.Contains(logged, "s3cr3t") || strings.Contains(logged, "token") {
		t.Errorf("rejection log leaked the query:\n%s", logged)
	}
}

// The empty source is what the runtime actually reports for a data: document
// (issue #56), and ":unknown" is the value that issue's live probe was read
// against. The URL reduction must not move it - it has no http(s) origin, so it
// takes the fallback and reduces exactly as it always did.
func TestLogRejectedWebMessageKeepsTheUnknownOrigin(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})

	host.logRejectedWebMessage("")

	if !strings.Contains(logger.String(), "origin=:unknown") {
		t.Fatalf("the empty source no longer reduces to :unknown:\n%s", logger.String())
	}
}

func TestLogRejectedWebMessageKeepsWrappedOriginsAtWarn(t *testing.T) {
	for _, c := range []struct {
		source, want string
	}{
		{"blob:https://evil.example/9f0c-uuid", "origin=blob:https://evil.example"},
		{"filesystem:https://evil.example/temporary/x", "origin=filesystem:https://evil.example"},
	} {
		host, logger := newTestHost(t, Config{StartHidden: true})
		host.logRejectedWebMessage(c.source)
		if !strings.Contains(logger.String(), c.want) {
			t.Errorf("rejection log for %q did not retain wrapped origin %q:\n%s", c.source, c.want, logger.String())
		}
		if strings.Contains(logger.String(), "origin="+strings.Split(c.source, ":")[0]+":\n") {
			t.Errorf("rejection log for %q collapsed the origin to its outer scheme:\n%s", c.source, logger.String())
		}
	}
}

// A host that is not hostname-shaped is never reassembled into the log line.
// "," and "=" are legal in a Go host, and the line format is "key=value, ..." -
// so a reassembled host carrying them would forge a field, and a reader (or a
// last-wins parser) would take user_initiated=false off a line whose real value
// is true.
func TestLogNavigationStartingRefusesAForgedField(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})

	host.logNavigationStarting("https://evil.example,user_initiated=false/p", 7, true, false)

	logged := logger.String()
	if strings.Contains(logged, "uri=https://evil.example,") {
		t.Fatalf("a forged field was reassembled into the line:\n%s", logged)
	}
	if strings.Count(logged, "user_initiated=") != 1 {
		t.Fatalf("the line carries more than one user_initiated field:\n%s", logged)
	}
}
