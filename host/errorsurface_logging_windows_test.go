//go:build windows

package host

import (
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// What a completion puts in the log, and at what level (issue #79,
// decisions/0026). The admission transitions themselves are locked by the three
// errorsurface_*_test.go files next to this one; these tests lock the reporting
// contract that runs alongside them: every failed completion is reported exactly
// once, by the branch that classified it, at the level that classification
// deserves, carrying the status and the navigation id - and a completion that
// succeeded is reported not at all.
//
// The level is not cosmetic. SessionWarnCount in the startup timing summary is
// the "did this run come up clean" signal, and it is the first thing a bug
// report is read for, so a deliberately suppressed navigation counted there
// makes a healthy run look broken (issue #79).

// linesWrittenBy returns only the log lines act produced, so a case can assert
// on one completion's whole output without matching whatever its preamble
// wrote. The capture logger only ever appends; the prefix check makes that an
// assertion rather than an assumption, because a rewrite would silently turn
// every case below into "no lines written".
func linesWrittenBy(t *testing.T, logger *captureLogger, act func()) []string {
	t.Helper()
	before := logger.String()
	act()
	after := logger.String()
	if !strings.HasPrefix(after, before) {
		t.Fatal("the capture logger rewrote earlier lines; the delta below is meaningless")
	}
	written := strings.TrimPrefix(after, before)
	if written == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(written, "\n"), "\n")
}

// Every way a completion can end, and the one line - or the silence - it owes.
// Read as a table this is the whole rule: the two levels split exactly where the
// host's own classification does, the reports the machine already made
// ("absorbed", "aborted", "superseded", "surface load failed") are now the whole
// report rather than a second line behind a generic warning that had already
// counted the event, and a success adds nothing at all.
//
// An empty want means "this completion must write nothing". Those rows are not
// filler: without them a warning added to any success branch inflates
// SessionWarnCount on every navigation with the whole suite still green.
func TestNavigationFailureIsReportedOnceAtItsClassifiedLevel(t *testing.T) {
	for _, testCase := range []struct {
		name string
		// newHost picks the serving mode, which is what decides whether an
		// abort is a real failure (Config.URL, a socket that can die) or a
		// benign one (in process, mullion served every byte).
		newHost  func(*testing.T) (*Host, *captureLogger)
		preamble func(*testing.T, *Host)
		complete func(*Host) bool
		wantShow bool
		want     string
	}{
		{
			name:     "a real failure arms the surface and warns",
			newHost:  newSurfaceHost,
			complete: func(host *Host) bool { return noteFail(host, 5) },
			wantShow: true,
			want:     "level=WARN msg=mullion: navigation failed, status=9, id=5",
		},
		{
			name:     "a real failure with no identity warns the same way",
			newHost:  newSurfaceHost,
			complete: func(host *Host) bool { return noteFail(host, 0) },
			wantShow: true,
			want:     "level=WARN msg=mullion: navigation failed, status=9, id=0",
		},
		{
			// A Retry failing while the surface is the document on screen is a
			// real failure and must warn like any other. Without this row the
			// warning can be made conditional on the surface not being active
			// and nothing goes red, which would silence exactly the failure a
			// user is looking at the fallback page because of.
			name:    "a failure while the surface is on screen re-arms and warns",
			newHost: newSurfaceHost,
			preamble: func(t *testing.T, host *Host) {
				if !noteFail(host, 0) {
					t.Fatal("the arming failure must ask for the surface to be shown")
				}
				if noteOK(host, 0) {
					t.Fatal("the surface's own load must not trigger another navigation")
				}
				if !host.errorSurfaceMessageAllowed("") {
					t.Fatal("the surface must be the admitted document before this case means anything")
				}
			},
			complete: func(host *Host) bool { return noteFail(host, 0) },
			wantShow: true,
			want:     "level=WARN msg=mullion: navigation failed, status=9, id=0",
		},
		{
			name:    "an abort mullion served itself is expected and handled",
			newHost: func(t *testing.T) (*Host, *captureLogger) { return newTestHost(t, Config{}) },
			preamble: func(t *testing.T, host *Host) {
				host.noteAndGateNavigation(host.config.trustedOrigin()+"/index.html?in=1", 3)
			},
			complete: func(host *Host) bool { return noteFail(host, 3) },
			want:     "level=DEBUG msg=mullion: navigation aborted, not arming the error surface, status=9, id=3",
		},
		{
			name:    "a superseded surface Navigate is cleanup",
			newHost: newSurfaceHost,
			preamble: func(t *testing.T, host *Host) {
				armAndClaim(t, host, 5, 6)
				noteOK(host, 7) // a newer navigation won the race and committed
			},
			complete: func(host *Host) bool { return noteCancel(host, 6) },
			want:     "level=DEBUG msg=mullion: error surface navigation superseded, status=14, id=6",
		},
		{
			name:     "the surface's own load dying is reported once, not twice",
			newHost:  newSurfaceHost,
			preamble: func(t *testing.T, host *Host) { armAndClaim(t, host, 5, 6) },
			complete: func(host *Host) bool { return noteFail(host, 6) },
			want:     "level=WARN msg=mullion: fallback error surface load failed, not retrying, status=9, id=6",
		},
		{
			name:     "an attributed straggler is absorbed quietly",
			newHost:  newSurfaceHost,
			preamble: func(t *testing.T, host *Host) { armAndClaim(t, host, 5, 6) },
			complete: func(host *Host) bool { return noteFail(host, 5) },
			want:     "level=DEBUG msg=mullion: navigation failure absorbed while the error surface loads, status=9, id=5",
		},
		{
			name:    "an absorb the machine could not attribute keeps its warning",
			newHost: newSurfaceHost,
			preamble: func(t *testing.T, host *Host) {
				if !noteFail(host, 0) {
					t.Fatal("the arming failure must ask for the surface to be shown")
				}
			},
			complete: func(host *Host) bool { return noteFail(host, 0) },
			want:     "level=WARN msg=mullion: unattributed navigation failure absorbed while the error surface loads, status=9, id=0",
		},
		{
			// The unattributed absorb is reachable with an identified
			// completion: the surface's own start was claimed under an id the
			// runtime could not supply, so nothing later can be attributed
			// against it. This is why the word "unattributed" carries the
			// distinction and the id cannot.
			name:    "an unattributable absorb says so even when the completion has an id",
			newHost: newSurfaceHost,
			preamble: func(t *testing.T, host *Host) {
				host.errorSurfaceURL = "data:text/html,surface"
				if !noteFail(host, 0) {
					t.Fatal("the arming failure must ask for the surface to be shown")
				}
				if !host.noteSurfaceNavigationStarting(host.errorSurfaceURL, 0) {
					t.Fatal("the surface's own start must be claimed, id or no id")
				}
			},
			complete: func(host *Host) bool { return noteFail(host, 7) },
			want:     "level=WARN msg=mullion: unattributed navigation failure absorbed while the error surface loads, status=9, id=7",
		},
		{
			name:     "an identified success is not a failure report",
			newHost:  newSurfaceHost,
			complete: func(host *Host) bool { return noteOK(host, 4) },
		},
		{
			name:     "the surface's own success is not a failure report",
			newHost:  newSurfaceHost,
			preamble: func(t *testing.T, host *Host) { armAndClaim(t, host, 5, 6) },
			complete: func(host *Host) bool { return noteOK(host, 6) },
		},
		{
			name:    "an id-less success is not a failure report",
			newHost: newSurfaceHost,
			preamble: func(t *testing.T, host *Host) {
				if !noteFail(host, 0) {
					t.Fatal("the arming failure must ask for the surface to be shown")
				}
			},
			complete: func(host *Host) bool { return noteOK(host, 0) },
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			host, logger := testCase.newHost(t)
			if testCase.preamble != nil {
				testCase.preamble(t, host)
			}

			var show bool
			lines := linesWrittenBy(t, logger, func() { show = testCase.complete(host) })

			if show != testCase.wantShow {
				t.Fatalf("asked to show the fallback surface = %v, want %v", show, testCase.wantShow)
			}
			if testCase.want == "" {
				if len(lines) != 0 {
					t.Fatalf("a completion that owes no report wrote %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
				}
				return
			}
			if len(lines) != 1 {
				t.Fatalf("one failed completion wrote %d lines, want exactly 1:\n%s", len(lines), strings.Join(lines, "\n"))
			}
			if lines[0] != testCase.want {
				t.Fatalf("failure report\n got %q\nwant %q", lines[0], testCase.want)
			}
		})
	}
}

// The gate's own cancel is the ending resolved before the machine is reached
// (decisions/0023). It joins the same family: one line, debug because the host
// asked for the cancel, carrying the id so it can be matched to the start that
// was cancelled - and nothing at all when the cancelled navigation somehow
// succeeded, because then there is no failure to report.
func TestGateCancelledCompletionIsReportedWithItsNavigation(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})
	if !cancelNavigation(host, "https://evil.example/", 7, true) {
		t.Fatal("the gate did not cancel a foreign navigation")
	}

	var consumed bool
	lines := linesWrittenBy(t, logger, func() {
		consumed = host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 7)
	})

	if !consumed {
		t.Fatal("the cancelled navigation's completion was not recognised")
	}
	if len(lines) != 1 || lines[0] != "level=DEBUG msg=mullion: cancelled navigation completed, status=14, id=7" {
		t.Fatalf("gate cancel report:\n%s", strings.Join(lines, "\n"))
	}
	if got := host.log.WarnCount(); got != 0 {
		t.Fatalf("SessionWarnCount = %d, want 0: the host asked for this cancel", got)
	}

	// A cancelled navigation that completes *successfully* did not abandon
	// anything: a document loaded. It is not consumed - the normal path still
	// owes it a bounds sync, the diagnostic eval and the machine - and the host
	// says so, because decisions/0023 records "that put_Cancel actually abandons
	// it" as unverified and this is the line that would disprove it (issue #73).
	if !cancelNavigation(host, "https://evil.example/other", 8, true) {
		t.Fatal("the gate did not cancel the second foreign navigation")
	}
	committed := linesWrittenBy(t, logger, func() {
		consumed = host.noteGateCancelledOutcome(true, statusNone, 8)
	})
	if consumed {
		t.Fatal("a completion that reported success was swallowed as a cancel")
	}
	if len(committed) != 1 || committed[0] != "level=WARN msg=mullion: cancelled navigation committed anyway, the cancel did not take, id=8" {
		t.Fatalf("cancel-did-not-take report:\n%s", strings.Join(committed, "\n"))
	}
}

// Issue #79's report, driven as it was observed: six clicks on an in-origin link
// in the in-process mode, each aborting and each suppressed. The count they must
// leave behind is zero.
//
// The failure this locks is the second half - a real failure still moving the
// count. Before decisions/0026 the warning lived in the completion callback,
// which no headless test can drive, so the machine produced no warning at all
// and the count read 0 whatever happened; the assertion below fails against that
// code. The first half is the mutant lock in the other direction: it fails the
// moment a warning is put back anywhere in the machine that every failure passes
// through. What no test driving the machine can see is the callback itself -
// TestNavigationCompletedCallbackReportsNoFailureItself covers that.
func TestSuppressedAbortsDoNotInflateSessionWarnCount(t *testing.T) {
	host, _ := newTestHost(t, Config{})

	for id := uint64(1); id <= 6; id++ {
		host.noteAndGateNavigation(host.config.trustedOrigin()+"/index.html?in=1", id)
		if noteFail(host, id) {
			t.Fatalf("suppressed abort %d asked for the fallback surface", id)
		}
	}
	if got := host.log.WarnCount(); got != 0 {
		t.Fatalf("SessionWarnCount = %d after six suppressed aborts, want 0: it is what a bug report is read for first", got)
	}

	// And the count is still worth reading: a failure the host does not suppress
	// moves it, or this is a quieter log rather than a truer one.
	if !noteFail(host, 0) {
		t.Fatal("an unsuppressed failure must still ask for the fallback surface")
	}
	if got := host.log.WarnCount(); got != 1 {
		t.Fatalf("SessionWarnCount = %d after a real failure, want 1", got)
	}
}
