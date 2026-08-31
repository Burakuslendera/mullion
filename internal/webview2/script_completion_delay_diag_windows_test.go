//go:build windows && mullion_script_completion_delay_diag

package webview2

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type diagnosticMarkers struct {
	mu    sync.Mutex
	lines []string
	wake  chan struct{}
}

func newDiagnosticMarkers() *diagnosticMarkers {
	return &diagnosticMarkers{wake: make(chan struct{}, 16)}
}

func (markers *diagnosticMarkers) record(line string) {
	markers.mu.Lock()
	markers.lines = append(markers.lines, line)
	markers.mu.Unlock()
	markers.wake <- struct{}{}
}

func (markers *diagnosticMarkers) contains(fragment string) bool {
	markers.mu.Lock()
	defer markers.mu.Unlock()
	return strings.Contains(strings.Join(markers.lines, "\n"), fragment)
}

func (markers *diagnosticMarkers) text() string {
	markers.mu.Lock()
	defer markers.mu.Unlock()
	return strings.Join(markers.lines, "\n")
}

func (markers *diagnosticMarkers) waitFor(t *testing.T, fragment string) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for !markers.contains(fragment) {
		select {
		case <-markers.wake:
		case <-deadline.C:
			t.Fatalf("diagnostic markers never contained %q", fragment)
		}
	}
}

func TestTaggedRequiredCompletionDelayHandsOffGenuineInvokeWithoutHoldingRuntimeReference(t *testing.T) {
	markers := newDiagnosticMarkers()
	diagnostic, err := startRequiredScriptCompletionDelayDiagnostic(1, time.Second, markers.record)
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()

	handler := newRequiredScriptCompletionHandler()
	if handler == nil {
		t.Fatal("required completion handler unavailable")
	}
	serverAddRef(handler.this)
	if got := scriptCompletionInvoke(handler.this, sOK, 1); got != sOK {
		t.Fatalf("genuine Invoke = %#x, want S_OK", got)
	}
	serverRelease(handler.this)
	server := serverFor(handler.this)
	if server == nil || atomic.LoadInt32(&server.refs) != 1 {
		t.Fatalf("refs after Runtime Invoke/Release = %v, want package reference only", server)
	}
	select {
	case <-handler.done:
		t.Fatal("required completion was published before diagnostic release")
	default:
	}

	event, err := diagnostic.WaitHeld(time.Second)
	if err != nil || event.RequiredIndex != 1 {
		t.Fatalf("held event = %+v, err=%v, want required index 1", event, err)
	}
	if err := diagnostic.Release(event.RequiredIndex); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handler.done:
	case <-time.After(time.Second):
		t.Fatal("released real completion was not published")
	}
	if err := handler.result(); err != nil {
		t.Fatalf("published real completion: %v", err)
	}
	markers.waitFor(t, "state=real_callback_published_explicit_release, required_index=1")
	handler.release()
	if serverFor(handler.this) != nil {
		t.Fatal("published diagnostic completion remained rooted")
	}
	diagnostic.Close()
	select {
	case <-diagnostic.markerDone:
	case <-time.After(time.Second):
		t.Fatal("responsive marker dispatcher did not stop")
	}
	beforeDrop, _ := diagnostic.MarkerDeliveryStats()
	diagnostic.emit("after_coordinator_close", 0)
	afterDrop, _ := diagnostic.MarkerDeliveryStats()
	if afterDrop != beforeDrop+1 {
		t.Fatalf("post-close marker drops = %d then %d, want one explicit drop", beforeDrop, afterDrop)
	}
	logged := markers.text()
	prior := -1
	for _, state := range []string{
		"state=real_callback_held, required_index=1",
		"state=release_requested, required_index=1",
		"state=real_callback_published_explicit_release, required_index=1",
		"state=coordinator_closed, required_index=0",
	} {
		index := strings.Index(logged, state)
		if index <= prior {
			t.Fatalf("diagnostic marker %q is out of order:\n%s", state, logged)
		}
		prior = index
	}
}

func TestTaggedDelayNeverTouchesOptionalScriptCompletion(t *testing.T) {
	diagnostic, err := startRequiredScriptCompletionDelayDiagnostic(1, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()

	handler := newScriptCompletionHandler()
	if handler == nil {
		t.Fatal("optional completion handler unavailable")
	}
	defer handler.release()
	if got := scriptCompletionInvoke(handler.this, sOK, 1); got != sOK {
		t.Fatalf("optional Invoke = %#x, want S_OK", got)
	}
	select {
	case <-handler.done:
	default:
		t.Fatal("tagged diagnostic delayed optional completion")
	}
	select {
	case event := <-diagnostic.held:
		t.Fatalf("optional completion produced held event %+v", event)
	default:
	}
}

func TestTaggedDelayFailSafePublishesRealCompletionAfterBound(t *testing.T) {
	markers := newDiagnosticMarkers()
	diagnostic, err := startRequiredScriptCompletionDelayDiagnostic(1, 10*time.Millisecond, markers.record)
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()

	handler := newRequiredScriptCompletionHandler()
	if handler == nil {
		t.Fatal("required completion handler unavailable")
	}
	defer handler.release()
	if got := scriptCompletionInvoke(handler.this, sOK, 1); got != sOK {
		t.Fatalf("required Invoke = %#x, want S_OK", got)
	}
	if _, err := diagnostic.WaitHeld(time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handler.done:
	case <-time.After(time.Second):
		t.Fatal("fail-safe did not publish the real completion")
	}
	markers.waitFor(t, "state=real_callback_published_failsafe_timeout, required_index=1")
}

func TestTaggedBlockingMarkerCannotDelayFailsafePublicationOrDispatcherShutdown(t *testing.T) {
	markerEntered := make(chan struct{})
	unblockMarker := make(chan struct{})
	markerExited := make(chan struct{})
	var markerOnce sync.Once
	var unblockOnce sync.Once
	unblock := func() { unblockOnce.Do(func() { close(unblockMarker) }) }
	diagnostic, err := startRequiredScriptCompletionDelayDiagnostic(1, 10*time.Millisecond, func(string) {
		markerOnce.Do(func() { close(markerEntered) })
		<-unblockMarker
		close(markerExited)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()
	defer unblock()

	handler := newRequiredScriptCompletionHandler()
	if handler == nil {
		t.Fatal("required completion handler unavailable")
	}
	defer handler.release()
	if got := scriptCompletionInvoke(handler.this, sOK, 1); got != sOK {
		t.Fatalf("required Invoke = %#x, want S_OK", got)
	}
	if _, err := diagnostic.WaitHeld(time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-markerEntered:
	case <-time.After(time.Second):
		t.Fatal("blocking marker callback was not entered")
	}
	select {
	case <-handler.done:
	case <-time.After(time.Second):
		t.Fatal("blocking marker delayed fail-safe publication")
	}
	diagnostic.Close()
	select {
	case <-diagnostic.markerDone:
	case <-time.After(time.Second):
		t.Fatal("marker dispatcher did not stop after its bounded callback timeout")
	}
	dropped, timedOut := diagnostic.MarkerDeliveryStats()
	if timedOut != 1 || dropped == 0 {
		t.Fatalf("marker delivery stats dropped=%d timedOut=%d, want at least one drop and exactly one timeout", dropped, timedOut)
	}

	fresh, err := startRequiredScriptCompletionDelayDiagnostic(1, time.Second, nil)
	if err != nil {
		t.Fatalf("fresh coordinator after blocked-marker close: %v", err)
	}
	fresh.Close()
	unblock()
	select {
	case <-markerExited:
	case <-time.After(time.Second):
		t.Fatal("released blocking marker goroutine did not exit")
	}
}

func TestTaggedBlockingMarkerCannotDelayClosePublication(t *testing.T) {
	markerEntered := make(chan struct{})
	unblockMarker := make(chan struct{})
	markerExited := make(chan struct{})
	var markerOnce sync.Once
	var unblockOnce sync.Once
	unblock := func() { unblockOnce.Do(func() { close(unblockMarker) }) }
	diagnostic, err := startRequiredScriptCompletionDelayDiagnostic(1, time.Second, func(string) {
		markerOnce.Do(func() { close(markerEntered) })
		<-unblockMarker
		close(markerExited)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()
	defer unblock()

	handler := newRequiredScriptCompletionHandler()
	if handler == nil {
		t.Fatal("required completion handler unavailable")
	}
	defer handler.release()
	if got := scriptCompletionInvoke(handler.this, sOK, 1); got != sOK {
		t.Fatalf("required Invoke = %#x, want S_OK", got)
	}
	if _, err := diagnostic.WaitHeld(time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-markerEntered:
	case <-time.After(time.Second):
		t.Fatal("blocking marker callback was not entered")
	}
	closed := make(chan struct{})
	go func() {
		diagnostic.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("blocking marker deadlocked coordinator Close")
	}
	select {
	case <-handler.done:
	case <-time.After(time.Second):
		t.Fatal("blocking marker delayed close publication")
	}
	select {
	case <-diagnostic.markerDone:
	case <-time.After(time.Second):
		t.Fatal("marker dispatcher did not stop after close")
	}
	unblock()
	select {
	case <-markerExited:
	case <-time.After(time.Second):
		t.Fatal("released blocking marker goroutine did not exit")
	}
}

func TestTaggedDiagnosticCloseReleasesHeldCompletionAndAllowsFreshCoordinator(t *testing.T) {
	markers := newDiagnosticMarkers()
	diagnostic, err := startRequiredScriptCompletionDelayDiagnostic(1, time.Second, markers.record)
	if err != nil {
		t.Fatal(err)
	}

	handler := newRequiredScriptCompletionHandler()
	if handler == nil {
		t.Fatal("required completion handler unavailable")
	}
	defer handler.release()
	if got := scriptCompletionInvoke(handler.this, sOK, 1); got != sOK {
		t.Fatalf("required Invoke = %#x, want S_OK", got)
	}
	if _, err := diagnostic.WaitHeld(time.Second); err != nil {
		t.Fatal(err)
	}
	diagnostic.Close()
	diagnostic.Close()
	select {
	case <-handler.done:
	case <-time.After(time.Second):
		t.Fatal("coordinator close did not release the held real completion")
	}
	markers.waitFor(t, "state=real_callback_published_coordinator_close, required_index=1")

	fresh, err := startRequiredScriptCompletionDelayDiagnostic(1, time.Second, nil)
	if err != nil {
		t.Fatalf("fresh coordinator after close: %v", err)
	}
	fresh.Close()
}

func TestTaggedDelayedPublicationCannotBeatCancellationAndAbandonment(t *testing.T) {
	markers := newDiagnosticMarkers()
	diagnostic, err := startRequiredScriptCompletionDelayDiagnostic(1, time.Second, markers.record)
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()

	handler := newRequiredScriptCompletionHandler()
	if handler == nil {
		t.Fatal("required completion handler unavailable")
	}
	serverAddRef(handler.this)
	if got := scriptCompletionInvoke(handler.this, sOK, 1); got != sOK {
		t.Fatalf("required Invoke = %#x, want S_OK", got)
	}
	serverRelease(handler.this)
	event, err := diagnostic.WaitHeld(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cancelled := make(chan struct{})
	close(cancelled)
	pumpSteps, pumpFinishes := 0, 0
	if _, err := waitForRequiredScriptCompletion(handler.done, cancelled, time.Second, "delayed required completion", func() bool {
		return false
	}, func() bool {
		pumpSteps++
		return false
	}, func() {
		pumpFinishes++
	}); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancelled delayed wait error = %v", err)
	}
	if pumpSteps != 0 || pumpFinishes != 1 {
		t.Fatalf("cancelled delayed wait steps=%d finishes=%d, want 0 and 1", pumpSteps, pumpFinishes)
	}
	handler.abandon()
	handler.release()
	if err := diagnostic.Release(event.RequiredIndex); err != nil {
		t.Fatal(err)
	}
	markers.waitFor(t, "state=publication_suppressed_explicit_release, required_index=1")
	select {
	case <-handler.done:
		t.Fatal("abandoned completion was published into the barrier")
	default:
	}
	if serverFor(handler.this) != nil {
		t.Fatal("abandoned delayed handler remained rooted")
	}
}

func TestTaggedDiagnosticRejectsConcurrentCoordinatorAndDuplicateRelease(t *testing.T) {
	diagnostic, err := startRequiredScriptCompletionDelayDiagnostic(1, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()
	if _, err := startRequiredScriptCompletionDelayDiagnostic(1, time.Second, nil); err == nil {
		t.Fatal("concurrent diagnostic coordinator was accepted")
	}

	handler := newRequiredScriptCompletionHandler()
	if handler == nil {
		t.Fatal("required completion handler unavailable")
	}
	defer handler.release()
	if got := scriptCompletionInvoke(handler.this, sOK, 1); got != sOK {
		t.Fatalf("required Invoke = %#x, want S_OK", got)
	}
	event, err := diagnostic.WaitHeld(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := diagnostic.Release(event.RequiredIndex); err != nil {
		t.Fatal(err)
	}
	if err := diagnostic.Release(event.RequiredIndex); err == nil {
		t.Fatal("duplicate diagnostic release was accepted")
	}
	select {
	case <-handler.done:
	case <-time.After(time.Second):
		t.Fatal("released completion was not published")
	}
}

func TestTaggedDiagnosticMarkerPanicCannotStrandHeldCompletion(t *testing.T) {
	diagnostic, err := startRequiredScriptCompletionDelayDiagnostic(1, time.Second, func(string) {
		panic("diagnostic marker failed")
	})
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()

	handler := newRequiredScriptCompletionHandler()
	if handler == nil {
		t.Fatal("required completion handler unavailable")
	}
	defer handler.release()
	if got := scriptCompletionInvoke(handler.this, sOK, 1); got != sOK {
		t.Fatalf("required Invoke = %#x, want S_OK", got)
	}
	event, err := diagnostic.WaitHeld(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := diagnostic.Release(event.RequiredIndex); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handler.done:
	case <-time.After(time.Second):
		t.Fatal("panicking marker stranded the real completion")
	}
}
