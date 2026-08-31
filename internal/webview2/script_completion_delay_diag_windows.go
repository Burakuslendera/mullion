//go:build windows && mullion_script_completion_delay_diag

package webview2

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	issue135DiagnosticHoldCount         = 2
	issue135DiagnosticMaxHold           = 10 * time.Second
	issue135DiagnosticMarkerQueue       = 16
	issue135DiagnosticMarkerCallTimeout = 100 * time.Millisecond
)

// RequiredScriptCompletionDelayEvent identifies one genuine required-script
// Runtime callback whose publication is being held by the diagnostic build.
type RequiredScriptCompletionDelayEvent struct {
	RequiredIndex int
}

// RequiredScriptCompletionDelayDiagnostic coordinates Issue #135's tagged live
// proof. It is internal to this module, inactive until the diagnostic command
// starts it, and cannot affect optional document-created scripts.
type RequiredScriptCompletionDelayDiagnostic struct {
	mu         sync.Mutex
	publishers sync.WaitGroup

	holdCount int
	maxHold   time.Duration
	held      chan RequiredScriptCompletionDelayEvent
	stop      chan struct{}
	releases  map[int]chan struct{}
	next      int
	closed    bool

	markerQueue    chan string
	markerStop     chan struct{}
	markerDone     chan struct{}
	markerMu       sync.Mutex
	markerClosed   bool
	markerDropped  uint64
	markerTimedOut uint64
}

var requiredScriptCompletionDelayDiagnostic struct {
	sync.Mutex
	active *RequiredScriptCompletionDelayDiagnostic
}

// StartIssue135ScriptCompletionDelayDiagnostic activates the tag-only,
// two-callback coordinator used by the diagnostic command. A second concurrent
// coordinator is rejected so one process cannot misattribute held callbacks.
func StartIssue135ScriptCompletionDelayDiagnostic(marker func(string)) (*RequiredScriptCompletionDelayDiagnostic, error) {
	return startRequiredScriptCompletionDelayDiagnostic(
		issue135DiagnosticHoldCount,
		issue135DiagnosticMaxHold,
		marker,
	)
}

func startRequiredScriptCompletionDelayDiagnostic(holdCount int, maxHold time.Duration, marker func(string)) (*RequiredScriptCompletionDelayDiagnostic, error) {
	if holdCount <= 0 || maxHold <= 0 {
		return nil, errors.New("webview2: invalid required-script completion diagnostic bounds")
	}
	diagnostic := &RequiredScriptCompletionDelayDiagnostic{
		holdCount: holdCount,
		maxHold:   maxHold,
		held:      make(chan RequiredScriptCompletionDelayEvent, holdCount),
		stop:      make(chan struct{}),
		releases:  make(map[int]chan struct{}, holdCount),
	}
	if marker != nil {
		diagnostic.markerQueue = make(chan string, issue135DiagnosticMarkerQueue)
		diagnostic.markerStop = make(chan struct{})
		diagnostic.markerDone = make(chan struct{})
	}
	requiredScriptCompletionDelayDiagnostic.Lock()
	if requiredScriptCompletionDelayDiagnostic.active != nil {
		requiredScriptCompletionDelayDiagnostic.Unlock()
		return nil, errors.New("webview2: required-script completion diagnostic already active")
	}
	requiredScriptCompletionDelayDiagnostic.active = diagnostic
	requiredScriptCompletionDelayDiagnostic.Unlock()
	if marker != nil {
		go diagnostic.dispatchMarkers(marker)
	}
	return diagnostic, nil
}

// WaitHeld returns only after a real Runtime Invoke has handed a required
// completion to the bounded publication delay.
func (diagnostic *RequiredScriptCompletionDelayDiagnostic) WaitHeld(timeout time.Duration) (RequiredScriptCompletionDelayEvent, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case event := <-diagnostic.held:
		return event, nil
	case <-diagnostic.stop:
		return RequiredScriptCompletionDelayEvent{}, errors.New("webview2: required-script completion diagnostic closed")
	case <-timer.C:
		return RequiredScriptCompletionDelayEvent{}, errors.New("webview2: gave up waiting for held required-script completion")
	}
}

// Release publishes the named real completion. Duplicate, unknown, and
// post-close releases fail instead of accidentally releasing another callback.
func (diagnostic *RequiredScriptCompletionDelayDiagnostic) Release(requiredIndex int) error {
	diagnostic.mu.Lock()
	if diagnostic.closed {
		diagnostic.mu.Unlock()
		return errors.New("webview2: required-script completion diagnostic is closed")
	}
	release := diagnostic.releases[requiredIndex]
	if release == nil {
		diagnostic.mu.Unlock()
		return fmt.Errorf("webview2: required-script completion %d is not held", requiredIndex)
	}
	delete(diagnostic.releases, requiredIndex)
	diagnostic.emit("release_requested", requiredIndex)
	close(release)
	diagnostic.mu.Unlock()
	return nil
}

// Close fail-safely releases every held publication and deactivates the
// process-wide coordinator. Handler abandonment still prevents a torn-down
// Browser from accepting a release as success.
func (diagnostic *RequiredScriptCompletionDelayDiagnostic) Close() {
	requiredScriptCompletionDelayDiagnostic.Lock()
	if requiredScriptCompletionDelayDiagnostic.active == diagnostic {
		requiredScriptCompletionDelayDiagnostic.active = nil
	}
	requiredScriptCompletionDelayDiagnostic.Unlock()

	diagnostic.mu.Lock()
	emitClosed := false
	if !diagnostic.closed {
		diagnostic.closed = true
		close(diagnostic.stop)
		emitClosed = true
	}
	diagnostic.mu.Unlock()
	if emitClosed {
		if diagnostic.markerStop != nil {
			go func() {
				diagnostic.publishers.Wait()
				diagnostic.emit("coordinator_closed", 0)
				diagnostic.markerMu.Lock()
				diagnostic.markerClosed = true
				close(diagnostic.markerStop)
				diagnostic.markerMu.Unlock()
			}()
		}
	}
}

// MarkerDeliveryStats reports tag-only observer failures. Marker delivery is
// never allowed to delay a real Runtime completion publication.
func (diagnostic *RequiredScriptCompletionDelayDiagnostic) MarkerDeliveryStats() (dropped, timedOut uint64) {
	return atomic.LoadUint64(&diagnostic.markerDropped), atomic.LoadUint64(&diagnostic.markerTimedOut)
}

func delayRequiredScriptCompletionPublication(publish func() bool) bool {
	requiredScriptCompletionDelayDiagnostic.Lock()
	diagnostic := requiredScriptCompletionDelayDiagnostic.active
	if diagnostic == nil {
		requiredScriptCompletionDelayDiagnostic.Unlock()
		return false
	}
	diagnostic.mu.Lock()
	if diagnostic.closed || diagnostic.next >= diagnostic.holdCount {
		diagnostic.mu.Unlock()
		requiredScriptCompletionDelayDiagnostic.Unlock()
		return false
	}
	diagnostic.next++
	requiredIndex := diagnostic.next
	release := make(chan struct{})
	diagnostic.releases[requiredIndex] = release
	diagnostic.publishers.Add(1)
	diagnostic.mu.Unlock()
	requiredScriptCompletionDelayDiagnostic.Unlock()

	go diagnostic.holdAndPublish(requiredIndex, release, publish)
	return true
}

func (diagnostic *RequiredScriptCompletionDelayDiagnostic) holdAndPublish(requiredIndex int, release <-chan struct{}, publish func() bool) {
	defer diagnostic.publishers.Done()
	timer := time.NewTimer(diagnostic.maxHold)
	defer timer.Stop()

	// emit only enqueues to the bounded observer dispatcher. The held channel is
	// sized to holdCount, so neither notification can postpone the fail-safe.
	diagnostic.emit("real_callback_held", requiredIndex)
	diagnostic.held <- RequiredScriptCompletionDelayEvent{RequiredIndex: requiredIndex}

	cause := "explicit_release"
	select {
	case <-release:
	case <-diagnostic.stop:
		cause = "coordinator_close"
	case <-timer.C:
		cause = "failsafe_timeout"
		diagnostic.mu.Lock()
		if diagnostic.releases[requiredIndex] == nil {
			if diagnostic.closed {
				cause = "coordinator_close"
			} else {
				cause = "explicit_release"
			}
		} else {
			delete(diagnostic.releases, requiredIndex)
		}
		diagnostic.mu.Unlock()
	}
	state := "real_callback_published_" + cause
	if !publish() {
		state = "publication_suppressed_" + cause
	}
	diagnostic.emit(state, requiredIndex)
}

func (diagnostic *RequiredScriptCompletionDelayDiagnostic) emit(state string, requiredIndex int) {
	if diagnostic.markerQueue == nil {
		return
	}
	message := fmt.Sprintf("mullion: issue135 script completion diagnostic, state=%s, required_index=%d", state, requiredIndex)
	diagnostic.markerMu.Lock()
	defer diagnostic.markerMu.Unlock()
	if diagnostic.markerClosed {
		atomic.AddUint64(&diagnostic.markerDropped, 1)
		return
	}
	select {
	case diagnostic.markerQueue <- message:
	default:
		atomic.AddUint64(&diagnostic.markerDropped, 1)
	}
}

func (diagnostic *RequiredScriptCompletionDelayDiagnostic) dispatchMarkers(marker func(string)) {
	defer close(diagnostic.markerDone)
	markerAvailable := true
	deliver := func(message string) {
		if !markerAvailable {
			atomic.AddUint64(&diagnostic.markerDropped, 1)
			return
		}
		returned := make(chan struct{})
		go func() {
			defer close(returned)
			defer func() { _ = recover() }()
			marker(message)
		}()
		timer := time.NewTimer(issue135DiagnosticMarkerCallTimeout)
		defer timer.Stop()
		select {
		case <-returned:
		case <-timer.C:
			atomic.AddUint64(&diagnostic.markerTimedOut, 1)
			markerAvailable = false
		}
	}

	for {
		select {
		case message := <-diagnostic.markerQueue:
			deliver(message)
		case <-diagnostic.markerStop:
			// Close never waits for observers. Drain the already accepted finite
			// queue here so responsive callbacks normally retain marker order.
			for {
				select {
				case message := <-diagnostic.markerQueue:
					deliver(message)
				default:
					return
				}
			}
		}
	}
}
