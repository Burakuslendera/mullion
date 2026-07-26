//go:build windows

package host

import "testing"

// Decision 0026's ordering rule, driven rather than described: every report
// comes after the state transition it describes, never before.
//
// The rule exists because a log call hands control to the embedder's Logger,
// which is arbitrary code. A Logger that pumps messages - a MessageBox, a GUI
// toolkit's own loop - has a queued navigation completion dispatched inside it,
// re-entering this machine. Report first and that nested completion is
// classified against a machine that has not transitioned yet: it arms a whole
// generation of its own, and the outer writes then land on top of what it
// claimed.
//
// 0026 wrote the rule and applied it at both of its own sites, and locked
// neither. An audit put the warning back in front of the four state writes in
// armErrorSurface, and in front of the seal's admission drop in
// noteSurfaceOwnOutcome, with the entire suite green both times - while the same
// rule in the cancelled-navigation ledger next door was driven from the start by
// TestEvictionSurvivesALoggerThatReentersTheLedger. These two are the
// error-surface machine's half of it.
//
// reentrantLogger, the Logger that runs a hook from inside Warn, is defined with
// that test in navigationledger_windows_test.go.

// newReentrantSurfaceHost is newSurfaceHost with a Logger that re-enters. The
// serving mode is deliberately the same one: with Config.URL set, a
// ConnectionAborted completion is a real failure rather than a benign abort
// (decisions/0024), and both cases below need a failure that actually arms.
func newReentrantSurfaceHost(t *testing.T) (*Host, *reentrantLogger) {
	t.Helper()
	logger := &reentrantLogger{captureLogger: &captureLogger{}}
	host := New(Config{URL: testExternalURL, Logger: logger})
	stubExternalOpen(host)
	return host, logger
}

// The arming site. A completion re-entering the machine inside armErrorSurface's
// warning must find a machine that is already armed and absorb - exactly what it
// would have done before the warning was moved here at all. Arming instead means
// it asks for a second surface Navigate, and the four writes below the log line
// then destroy the generation it just claimed, which is issue #56's
// dead-caption-buttons symptom.
func TestArmingIsCommittedBeforeItIsReported(t *testing.T) {
	host, logger := newReentrantSurfaceHost(t)

	var nestedAskedToShow bool
	logger.onWarn = func() { nestedAskedToShow = noteFail(host, 43) }

	if !noteFail(host, 42) {
		t.Fatal("the arming failure must ask for the surface to be shown")
	}
	if nestedAskedToShow {
		t.Fatal("a completion re-entering the machine inside the arming warning armed a second generation: the report ran ahead of the state it describes (decisions/0026)")
	}
}

// The seal site. The surface's own load dying drops the admission and then says
// so. A nested completion inside that warning arms a fresh generation, and the
// admission that generation claims has to survive the rest of the outer call -
// report first and the outer `errorSurfaceActive = false` lands on top of it,
// leaving a surface in flight that the machine will not admit, with no line
// anywhere saying why the caption buttons stopped answering.
func TestTheSealDropsTheAdmissionBeforeItIsReported(t *testing.T) {
	host, logger := newReentrantSurfaceHost(t)
	armAndClaim(t, host, 5, 6)

	logger.onWarn = func() {
		if !noteFail(host, 7) {
			t.Error("the nested failure must arm a generation of its own, or this case asserts nothing")
		}
	}

	if noteFail(host, 6) {
		t.Fatal("the surface's own load failing must not ask for another navigation")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the nested arming's admission was overwritten by the outer seal: the report ran ahead of the state it describes (decisions/0026)")
	}
}
