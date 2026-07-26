//go:build windows

package host

import (
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// The cancelled-navigation ledger as a data structure (issue #73,
// decisions/0027): what it holds, what it reuses, what it drops when it is full
// and how it says so. What a cancel *means* - that deciding commits to nothing,
// that an id-less one is still recognised, that an unreadable target is
// cancelled loudly - is next door in navigationcancel_windows_test.go.
//
// The pair of calls a real NavigationStarting makes is `cancelNavigation`
// (systembrowser_windows_test.go).

// outstandingCancels reports the ledger as a plain slice, so a test can say what
// it expects to be held rather than assert on the array's padding.
func outstandingCancels(host *Host) []uint64 {
	var live []uint64
	for _, id := range host.cancelledNavIDs {
		if id != 0 {
			live = append(live, id)
		}
	}
	return live
}

func wantOutstanding(t *testing.T, host *Host, want ...uint64) {
	t.Helper()
	got := outstandingCancels(host)
	if len(got) != len(want) {
		t.Fatalf("ledger holds %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ledger holds %v, want %v", got, want)
		}
	}
}

// More than one cancel can be outstanding. The single slot this replaced meant
// the second cancel evicted the first, and the evicted navigation's own
// OperationCanceled completion then reached the error-surface machine, armed it
// and tore the live frontend down into the fallback page - the exact failure the
// id consumption was added to prevent.
func TestLedgerHoldsSeveralOutstandingCancels(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	for id := uint64(5); id <= 8; id++ {
		if !cancelNavigation(host, "https://evil.example/", id, true) {
			t.Fatalf("the gate did not cancel navigation %d", id)
		}
	}
	wantOutstanding(t, host, 5, 6, 7, 8)

	// Completions arrive out of order, as completions do.
	for _, id := range []uint64{7, 5, 8, 6} {
		if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, id) {
			t.Fatalf("the completion of cancelled navigation %d was not recognised", id)
		}
	}
	wantOutstanding(t, host)
}

// A consumed entry frees its slot for real. The first version zeroed in place
// and appended by shifting the whole array, so the holes drifted along with
// everything else and an entry was dropped after four *later* cancels however
// many of them had completed - a live entry evicted with three slots standing
// empty, reported as "ledger full", its completion left to arm the fallback
// surface. That is the issue #73 failure, relocated rather than fixed.
func TestConsumedSlotsAreReusedBeforeAnythingIsEvicted(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	for id := uint64(1); id <= cancelledNavSlots; id++ {
		cancelNavigation(host, "https://evil.example/", id, true)
	}
	// Everything except the first completes, so one cancel is outstanding.
	for id := uint64(2); id <= cancelledNavSlots; id++ {
		if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, id) {
			t.Fatalf("the completion of cancelled navigation %d was not recognised", id)
		}
	}
	wantOutstanding(t, host, 1)

	cancelNavigation(host, "https://evil.example/", 99, true)

	wantOutstanding(t, host, 1, 99)
	if strings.Contains(logger.String(), "cancelled navigation forgotten") {
		t.Fatalf("a live entry was evicted while slots stood empty:\n%s", logger.String())
	}
	if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 1) {
		t.Fatal("the long-outstanding cancel was dropped even though the ledger had room")
	}
}

// When the ledger really is full, the entry it drops is the oldest one. That is
// what the compaction buys and the free-slot search does not: without closing
// the gap, a slot freed at the front is refilled by the newest cancel, and the
// eviction - which reads the front - then drops the *newest* entry while three
// older ones sit behind it.
func TestEvictionDropsTheOldestEntry(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	for id := uint64(1); id <= cancelledNavSlots; id++ {
		cancelNavigation(host, "https://evil.example/", id, true)
	}
	// The oldest completes, freeing the front, and a new cancel takes its place.
	if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 1) {
		t.Fatal("the oldest cancel's completion was not recognised")
	}
	cancelNavigation(host, "https://evil.example/", 5, true)
	wantOutstanding(t, host, 2, 3, 4, 5)

	// Full again. The one that goes must be 2, not the 5 that landed at the front.
	cancelNavigation(host, "https://evil.example/", 6, true)

	wantOutstanding(t, host, 3, 4, 5, 6)
	if !strings.Contains(logger.String(), "cancelled navigation forgotten, ledger full, id=2") {
		t.Fatalf("the eviction dropped something other than the oldest entry:\n%s", logger.String())
	}
}

// An outstanding cancel is the gate's business and the error surface's state is
// not part of it: a completion that belongs to a cancelled navigation must be
// consumed whether or not the surface happens to be in flight, or the machine
// starts seeing completions it was never meant to classify.
func TestTheLedgerIsConsultedWhateverTheSurfaceIsDoing(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})
	cancelNavigation(host, "https://evil.example/", 12, true)
	// Arm the surface: a real failure on another navigation puts it in flight.
	if !host.noteNavigationOutcome(false, statusNone, 13) {
		t.Fatal("the arming failure must ask for the surface to be shown")
	}
	if !host.errorSurfaceLoading {
		t.Fatal("the surface must be in flight for this case to mean anything")
	}

	if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 12) {
		t.Fatal("a cancelled navigation's completion must be consumed while the surface loads too")
	}
	wantOutstanding(t, host)
}

// The ledger is bounded, and reaching the bound is news rather than silence: the
// navigation dropped to make room reverts to the behaviour this issue is about,
// and nothing downstream could otherwise say which one it was.
func TestLedgerReportsWhatItForgets(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	for id := uint64(1); id <= cancelledNavSlots; id++ {
		cancelNavigation(host, "https://evil.example/", id, true)
	}
	if strings.Contains(logger.String(), "cancelled navigation forgotten") {
		t.Fatalf("the ledger reported an eviction before it was full:\n%s", logger.String())
	}

	cancelNavigation(host, "https://evil.example/", cancelledNavSlots+1, true)

	if !strings.Contains(logger.String(), "cancelled navigation forgotten, ledger full, id=1") {
		t.Fatalf("the evicted navigation was dropped silently:\n%s", logger.String())
	}
	wantOutstanding(t, host, 2, 3, 4, cancelledNavSlots+1)
	if host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 1) {
		t.Fatal("the evicted navigation was still in the ledger")
	}
}

// The id-less half is bounded the same way and says so the same way. It used to
// saturate in silence, so a dropped cancel there had nothing at all to attribute
// it to - while the other half warned.
func TestIdlessLedgerAlsoReportsWhatItForgets(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	for i := 0; i < cancelledNavSlots; i++ {
		cancelNavigation(host, "https://evil.example/", 0, true)
	}
	if strings.Contains(logger.String(), "cancelled navigation forgotten") {
		t.Fatalf("the id-less half reported an eviction before it was full:\n%s", logger.String())
	}

	cancelNavigation(host, "https://evil.example/", 0, true)

	if !strings.Contains(logger.String(), "cancelled navigation forgotten, ledger full and it has no id") {
		t.Fatalf("the id-less half saturated in silence:\n%s", logger.String())
	}
	if host.cancelledNavAnonymous != cancelledNavSlots {
		t.Fatalf("id-less count = %d, want %d", host.cancelledNavAnonymous, cancelledNavSlots)
	}
}

// A redirect fires NavigationStarting again under its navigation's original id.
// Without the dedup a chain books one entry per hop and evicts real cancels to
// do it, which is what the ledger was widened to prevent.
func TestRepeatedStartsUnderOneIdAreOneEntry(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	for i := 0; i < cancelledNavSlots+2; i++ {
		if !cancelNavigation(host, "https://evil.example/hop", 11, true) {
			t.Fatal("the gate did not cancel the off-origin navigation")
		}
	}

	wantOutstanding(t, host, 11)
	if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 11) {
		t.Fatal("the one navigation's completion was not recognised")
	}
	if host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 11) {
		t.Fatal("one navigation left more than one entry behind")
	}
}

// An id-ful completion never spends the id-less credit and vice versa. Identity
// is read separately at the start and at the completion and either read can
// fail, so the two halves can disagree about one navigation; when they do, the
// entry is stranded rather than matched against something else.
func TestTheTwoHalvesOfTheLedgerDoNotCrossMatch(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})
	cancelNavigation(host, "https://evil.example/", 7, true)
	cancelNavigation(host, "https://evil.example/", 0, true)

	// The id-ful entry is not spent by an id-less completion...
	if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 0) {
		t.Fatal("the id-less completion must spend the id-less credit")
	}
	wantOutstanding(t, host, 7)
	if host.cancelledNavAnonymous != 0 {
		t.Fatalf("id-less count = %d, want 0", host.cancelledNavAnonymous)
	}
	// ...and an id-ful completion does not spend a credit that is gone.
	if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 7) {
		t.Fatal("the id-ful completion must spend its own entry")
	}
	wantOutstanding(t, host)
}

// reentrantLogger runs a hook from inside Warn, which is what an embedder's
// Logger does when it pumps messages - a MessageBox, a GUI toolkit's own loop -
// and a queued navigation event is dispatched inside the call. Decision 0026
// established that every report must therefore follow the state transition it
// describes; this was the first test in the suite that actually drove it, and
// errorsurface_reentrancy_windows_test.go now drives 0026's own two sites with
// the same logger.
type reentrantLogger struct {
	*captureLogger
	onWarn func()
}

func (logger *reentrantLogger) Warn(message string) {
	logger.captureLogger.Warn(message)
	if hook := logger.onWarn; hook != nil {
		logger.onWarn = nil // once: the hook itself warns
		hook()
	}
}

// The eviction warning must come after the ledger has been rewritten. Logging
// first let the nested call see the array as it was, warn about the same entry a
// second time, and shift it again - so two entries went and only one was ever
// named. The entry nobody names is the one that silently reverts to the
// pre-issue-73 behaviour, which is precisely what the warning exists to prevent.
func TestEvictionSurvivesALoggerThatReentersTheLedger(t *testing.T) {
	capture := &captureLogger{}
	logger := &reentrantLogger{captureLogger: capture}
	host := New(Config{StartHidden: true, PinNavigationToOrigin: true, Logger: logger})
	stubExternalOpen(host)

	for id := uint64(1); id <= cancelledNavSlots; id++ {
		cancelNavigation(host, "https://evil.example/", id, true)
	}
	// The nested cancel lands in the middle of the outer one's eviction.
	logger.onWarn = func() { cancelNavigation(host, "https://evil.example/", 99, true) }
	cancelNavigation(host, "https://evil.example/", 5, true)

	// Both cancels are held, and the two evictions named different navigations.
	wantOutstanding(t, host, 3, 4, 5, 99)
	logged := capture.String()
	for _, want := range []string{"ledger full, id=1", "ledger full, id=2"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("an entry was evicted without being named (%q missing):\n%s", want, logged)
		}
	}
	if strings.Count(logged, "ledger full, id=1") != 1 {
		t.Fatalf("the same entry was named twice while another went unnamed:\n%s", logged)
	}
}
