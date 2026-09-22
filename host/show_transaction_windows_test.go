//go:build windows

package host

import (
	"errors"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

type errorReentrantVisibilityLogger struct {
	*captureLogger
	onError func()
}

func (logger *errorReentrantVisibilityLogger) Error(message string) {
	logger.captureLogger.Error(message)
	if hook := logger.onError; hook != nil {
		logger.onError = nil
		hook()
	}
}

func newTransactionalShowHost(t *testing.T) (*Host, windowHandle, *webview2.Browser) {
	t.Helper()
	host, _ := newTestHost(t, Config{})
	const hwnd = windowHandle(0x160)
	beginHeadlessLifecycleRun(t, host, hwnd)
	browser := &webview2.Browser{}
	host.browser = browser
	return host, hwnd, browser
}

func explicitShowIntent(t *testing.T, host *Host) uint64 {
	t.Helper()
	intent, ok := host.beginVisibilityShowIntent(false)
	if !ok {
		t.Fatal("explicit show intent refused")
	}
	return intent
}

func startupShowIntent(host *Host) uint64 {
	host.startupMu.Lock()
	defer host.startupMu.Unlock()
	host.visibilityGeneration = 1
	host.startupShowIntent = 1
	return 1
}

func TestControllerFailureNeverExposesHiddenParent(t *testing.T) {
	host, _, _ := newTransactionalShowHost(t)
	visible := false
	var effects []string
	host.applyControllerVisibility = func(bool) error {
		effects = append(effects, "controller-show")
		return errors.New("sentinel HRESULT")
	}
	host.applyParentVisibility = func(_ windowHandle, _ int32) error {
		effects = append(effects, "parent-show")
		visible = true
		return nil
	}
	host.queryParentVisible = func(windowHandle) bool { return visible }

	if got := host.applyShowAfterEnsure(explicitShowIntent(t, host)); got != showRetryableHidden {
		t.Fatalf("disposition = %v, want retryable-hidden", got)
	}
	if visible || len(effects) != 1 || effects[0] != "controller-show" {
		t.Fatalf("effects = %v visible=%v, want controller-only and hidden", effects, visible)
	}
}

func TestParentFailureRollsControllerBackBeforeRetry(t *testing.T) {
	host, _, _ := newTransactionalShowHost(t)
	visible := false
	var controller []bool
	host.applyControllerVisibility = func(value bool) error {
		controller = append(controller, value)
		return nil
	}
	host.applyParentVisibility = func(_ windowHandle, command int32) error {
		visible = command != swHide
		return nil
	}
	host.queryParentVisible = func(windowHandle) bool { return visible }
	host.applyParentUpdate = func(windowHandle) error { return errors.New("update failed") }

	if got := host.applyShowAfterEnsure(explicitShowIntent(t, host)); got != showRetryableHidden {
		t.Fatalf("disposition = %v, want retryable-hidden", got)
	}
	if visible || len(controller) != 2 || !controller[0] || controller[1] {
		t.Fatalf("rollback = controller %v visible=%v", controller, visible)
	}
}

func TestStartupShowAllowsOneRetryThenTerminates(t *testing.T) {
	host, hwnd, _ := newTransactionalShowHost(t)
	run := host.currentRun()
	host.startupShowStarted = true
	host.startupShowReleased = true
	intent := startupShowIntent(host)

	if terminal := host.resolveStartupShowFailure(hwnd, run.token, intent, showRetryableHidden); terminal {
		t.Fatal("first safe-hidden failure became terminal")
	}
	host.startupMu.Lock()
	firstFailures := host.startupShowFailures
	timer := host.startupShowTimer
	host.startupMu.Unlock()
	if firstFailures != 1 || timer == nil {
		t.Fatalf("first failure state = failures %d timer %v", firstFailures, timer != nil)
	}
	timer.Stop()
	host.startupMu.Lock()
	host.startupShowReleased = true
	host.startupMu.Unlock()

	if terminal := host.resolveStartupShowFailure(hwnd, run.token, intent, showRetryableHidden); !terminal {
		t.Fatal("second failure did not become terminal")
	}
}

func TestCancelledShowNeverRearmsStartupGate(t *testing.T) {
	host, hwnd, _ := newTransactionalShowHost(t)
	run := host.currentRun()
	host.startupShowStarted = true
	host.startupShowReleased = true
	intent := startupShowIntent(host)
	if terminal := host.resolveStartupShowFailure(hwnd, run.token, intent, showCancelled); terminal {
		t.Fatal("cancellation was promoted to a visibility terminal")
	}
	host.startupMu.Lock()
	defer host.startupMu.Unlock()
	if host.startupShowTimer != nil || host.startupShowFailures != 0 {
		t.Fatalf("cancelled show rearmed state: timer=%v failures=%d", host.startupShowTimer != nil, host.startupShowFailures)
	}
}

func TestPersistentStartupFailureTearsDownExactlyOnce(t *testing.T) {
	host, hwnd, _ := newTransactionalShowHost(t)
	run := host.currentRun()
	host.applyControllerVisibility = func(bool) error { return errors.New("persistent HRESULT") }
	host.queryParentVisible = func(windowHandle) bool { return false }
	var destroys int
	host.destroyNativeWindow = func(got windowHandle) {
		if got != hwnd {
			t.Fatalf("destroy HWND = %#x, want %#x", got, hwnd)
		}
		destroys++
	}
	host.startupShowStarted = true
	host.startupShowReleased = true
	startupShowIntent(host)

	host.windowProc(hwnd, wmNativeShow, startupShowCommand, run.token)
	host.startupMu.Lock()
	timer := host.startupShowTimer
	host.startupShowReleased = true
	host.startupMu.Unlock()
	if timer == nil {
		t.Fatal("first failure did not arm the one retry")
	}
	timer.Stop()
	host.windowProc(hwnd, wmNativeShow, startupShowCommand, run.token)
	host.windowProc(hwnd, wmNativeShow, startupShowCommand, run.token)
	if destroys != 1 || !errors.Is(host.terminalOutcome(), ErrWindowVisibilityUnavailable) {
		t.Fatalf("terminal = destroys %d outcome %v", destroys, host.terminalOutcome())
	}
}

func TestExplicitShowFailureDoesNotCreateStartupRetry(t *testing.T) {
	host, hwnd, _ := newTransactionalShowHost(t)
	run := host.currentRun()
	host.applyControllerVisibility = func(bool) error { return errors.New("explicit HRESULT") }
	host.queryParentVisible = func(windowHandle) bool { return false }
	host.windowProc(hwnd, wmNativeShow, 0, run.token)
	host.startupMu.Lock()
	defer host.startupMu.Unlock()
	if host.startupShowTimer != nil || host.startupShowFailures != 0 {
		t.Fatalf("explicit failure created automatic retry: timer=%v failures=%d", host.startupShowTimer != nil, host.startupShowFailures)
	}
}

func TestShowSuccessIsNotPublishedAfterBoundsReentrancyInvalidatesOwner(t *testing.T) {
	host, hwnd, _ := newTransactionalShowHost(t)
	host.applyControllerVisibility = func(bool) error { return nil }
	host.applyParentVisibility = func(windowHandle, int32) error { return nil }
	host.queryParentVisible = func(windowHandle) bool { return true }
	host.applyParentUpdate = func(windowHandle) error { return nil }
	host.applyParentForeground = func(windowHandle) error { return nil }
	host.syncWindowBounds = func(string) { host.beginWindowDestroy(hwnd) }
	if got := host.applyShowAfterEnsure(explicitShowIntent(t, host)); got != showCancelled {
		t.Fatalf("disposition = %v, want cancelled after owner invalidation", got)
	}
}

func TestNegativeShowTimeoutPostsItsOnlyRetryImmediately(t *testing.T) {
	host, hwnd, _ := newTransactionalShowHost(t)
	run := host.currentRun()
	host.config.ShowTimeout = -1
	host.startupShowStarted = true
	host.startupShowReleased = true
	intent := startupShowIntent(host)
	var posts int
	host.postNativeCommand = func(got windowHandle, message uint32, wParam, token uintptr) error {
		if got != hwnd || message != wmNativeShow || wParam != startupShowCommand || token != run.token {
			t.Fatalf("retry post = (%#x,%#x,%#x,%#x)", got, message, wParam, token)
		}
		posts++
		return nil
	}
	if terminal := host.resolveStartupShowFailure(hwnd, run.token, intent, showRetryableHidden); terminal {
		t.Fatal("first failure became terminal")
	}
	if posts != 1 {
		t.Fatalf("immediate retry posts = %d, want 1", posts)
	}
}

func TestTransientControllerFailureRecoversThroughProductionDispatch(t *testing.T) {
	host, hwnd, _ := newTransactionalShowHost(t)
	run := host.currentRun()
	host.startupShowStarted = true
	host.startupShowReleased = true
	startupShowIntent(host)
	visible := false
	controllerCalls := 0
	parentShows := 0
	host.applyControllerVisibility = func(value bool) error {
		if !value {
			return nil
		}
		controllerCalls++
		if controllerCalls == 1 {
			return errors.New("transient HRESULT")
		}
		return nil
	}
	host.applyParentVisibility = func(_ windowHandle, command int32) error {
		visible = command != swHide
		if visible {
			parentShows++
		}
		return nil
	}
	host.queryParentVisible = func(windowHandle) bool { return visible }
	host.applyParentUpdate = func(windowHandle) error { return nil }
	host.applyParentForeground = func(windowHandle) error { return nil }
	host.syncWindowBounds = func(string) {}
	if got := host.windowProc(hwnd, wmNativeShow, startupShowCommand, run.token); got != 0 {
		t.Fatalf("first attempt result = %d, want failure", got)
	}
	host.startupMu.Lock()
	timer := host.startupShowTimer
	host.startupShowReleased = true
	host.startupMu.Unlock()
	if timer == nil {
		t.Fatal("transient failure did not arm retry")
	}
	timer.Stop()
	if got := host.windowProc(hwnd, wmNativeShow, startupShowCommand, run.token); got != 1 {
		t.Fatalf("second attempt result = %d, want success", got)
	}
	if controllerCalls != 2 || parentShows != 1 || !visible {
		t.Fatalf("recovery effects = controller %d parent %d visible %v", controllerCalls, parentShows, visible)
	}
}

func TestVisibilityTerminalTeardownCancelsCreationAndStaleRetry(t *testing.T) {
	host, hwnd, _ := newTransactionalShowHost(t)
	run := host.currentRun()
	cancel := host.embedCancellation
	host.startupShowStarted = true
	host.startupShowReleased = true
	startupShowIntent(host)
	host.applyControllerVisibility = func(bool) error { return errors.New("persistent HRESULT") }
	host.queryParentVisible = func(windowHandle) bool { return false }
	var posts int
	host.postNativeCommand = func(windowHandle, uint32, uintptr, uintptr) error { posts++; return nil }
	host.destroyNativeWindow = func(got windowHandle) {
		host.beginWindowDestroy(got)
		host.windowDestroyTeardown()
	}
	host.windowProc(hwnd, wmNativeShow, startupShowCommand, run.token)
	host.startupMu.Lock()
	timer := host.startupShowTimer
	host.startupShowReleased = true
	host.startupMu.Unlock()
	if timer == nil {
		t.Fatal("first failure did not retain retry timer")
	}
	timer.Stop()
	host.windowProc(hwnd, wmNativeShow, startupShowCommand, run.token)
	select {
	case <-cancel:
	default:
		t.Fatal("terminal teardown did not close issue #154 cancellation")
	}
	host.fireStartupShowGate(timer, run)
	if posts != 0 {
		t.Fatalf("stale retry posted %d commands after teardown", posts)
	}
	host.startupMu.Lock()
	defer host.startupMu.Unlock()
	if host.startupShowTimer != nil {
		t.Fatal("terminal teardown retained startup timer")
	}
}

func TestHideInvalidatesPendingAndAlreadyPostedStartupRetry(t *testing.T) {
	host, hwnd, _ := newTransactionalShowHost(t)
	run := host.currentRun()
	host.startupShowStarted = true
	host.startupShowReleased = true
	intent := startupShowIntent(host)
	var postedWParam uintptr
	host.postNativeCommand = func(_ windowHandle, message uint32, wParam, _ uintptr) error {
		if message == wmNativeShow {
			postedWParam = wParam
		}
		return nil
	}
	if host.resolveStartupShowFailure(hwnd, run.token, intent, showRetryableHidden) {
		t.Fatal("first failure became terminal")
	}
	host.startupMu.Lock()
	timer := host.startupShowTimer
	host.startupMu.Unlock()
	if timer == nil {
		t.Fatal("first failure did not arm retry")
	}
	timer.Stop()
	host.fireStartupShowGate(timer, run)
	if postedWParam != startupShowCommand {
		t.Fatal("retry was not posted")
	}
	var applications int
	host.applyNativeCommand = func(windowHandle, uint32, uintptr) uintptr { applications++; return 1 }
	host.windowProc(hwnd, wmNativeHide, 0, run.token)
	applications = 0
	host.windowProc(hwnd, wmNativeShow, postedWParam, run.token)
	if applications != 0 {
		t.Fatalf("stale retry applied %d times after Hide", applications)
	}
	host.startupMu.Lock()
	defer host.startupMu.Unlock()
	if host.startupShowTimer != nil || host.startupShowIntent != 0 {
		t.Fatalf("Hide retained retry state: timer=%v intent=%d", host.startupShowTimer != nil, host.startupShowIntent)
	}
}

func TestControllerFailureLoggerReentrantShowOwnsFinalVisibility(t *testing.T) {
	logger := &reentrantLogger{captureLogger: &captureLogger{}}
	host := New(Config{Logger: logger})
	stubExternalOpen(host)
	const hwnd = windowHandle(0x161)
	run := beginHeadlessLifecycleRun(t, host, hwnd)
	host.browser = &webview2.Browser{}
	visible := false
	controllerCalls := 0
	host.applyControllerVisibility = func(value bool) error {
		if !value {
			t.Fatal("stale outer Show rolled back nested success")
		}
		controllerCalls++
		if controllerCalls == 1 {
			return errors.New("outer HRESULT")
		}
		return nil
	}
	host.applyParentVisibility = func(_ windowHandle, command int32) error { visible = command != swHide; return nil }
	host.queryParentVisible = func(windowHandle) bool { return visible }
	host.applyParentUpdate = func(windowHandle) error { return nil }
	host.applyParentForeground = func(windowHandle) error { return nil }
	host.syncWindowBounds = func(string) {}
	logger.onWarn = func() { host.windowProc(hwnd, wmNativeShow, 0, run.token) }
	host.windowProc(hwnd, wmNativeShow, 0, run.token)
	if !visible || controllerCalls != 2 {
		t.Fatalf("nested Show result = visible %v controller calls %d", visible, controllerCalls)
	}
}

func TestForegroundLoggerReentrantHideOwnsFinalVisibility(t *testing.T) {
	logger := &reentrantLogger{captureLogger: &captureLogger{}}
	host := New(Config{Logger: logger})
	stubExternalOpen(host)
	const hwnd = windowHandle(0x162)
	run := beginHeadlessLifecycleRun(t, host, hwnd)
	host.browser = &webview2.Browser{}
	visible := false
	host.applyControllerVisibility = func(bool) error { return nil }
	host.applyParentVisibility = func(_ windowHandle, command int32) error { visible = command != swHide; return nil }
	host.queryParentVisible = func(windowHandle) bool { return visible }
	host.applyParentUpdate = func(windowHandle) error { return nil }
	host.applyParentForeground = func(windowHandle) error { return errors.New("foreground refused") }
	logger.onWarn = func() { host.windowProc(hwnd, wmNativeHide, 0, run.token) }
	host.windowProc(hwnd, wmNativeShow, 0, run.token)
	if visible {
		t.Fatal("outer Show overrode reentrant Hide intent")
	}
}

func TestVisibilityIntentIsFreshAcrossSequentialRuns(t *testing.T) {
	host, hwnd, _ := newTransactionalShowHost(t)
	oldIntent := explicitShowIntent(t, host)
	oldRun := host.currentRun()
	host.browser = nil
	newRun := recycleHeadlessLifecycleRun(t, host, hwnd)
	newIntent := explicitShowIntent(t, host)
	if oldRun.token == newRun.token {
		t.Fatal("sequential Runs reused token")
	}
	if !host.visibilityIntentMatches(newIntent) {
		t.Fatal("new Run intent is not current")
	}
	if oldIntent != newIntent {
		// Generation may restart per Run; the active-Run token is the cross-Run owner.
		return
	}
	var applied int
	host.applyNativeCommand = func(windowHandle, uint32, uintptr) uintptr { applied++; return 1 }
	host.windowProc(hwnd, wmNativeShow, 0, oldRun.token)
	if applied != 0 {
		t.Fatal("old Run applied a Show in the new Run")
	}
}

func TestControllerHideFailureLoggerReentrantShowOwnsFinalVisibility(t *testing.T) {
	logger := &reentrantLogger{captureLogger: &captureLogger{}}
	host := New(Config{Logger: logger})
	stubExternalOpen(host)
	const hwnd = windowHandle(0x163)
	run := beginHeadlessLifecycleRun(t, host, hwnd)
	host.browser = &webview2.Browser{}
	visible := true
	parentHides := 0
	host.applyControllerVisibility = func(value bool) error {
		if !value {
			return errors.New("outer hide HRESULT")
		}
		return nil
	}
	host.applyParentVisibility = func(_ windowHandle, command int32) error {
		visible = command != swHide
		if !visible {
			parentHides++
		}
		return nil
	}
	host.queryParentVisible = func(windowHandle) bool { return visible }
	host.applyParentUpdate = func(windowHandle) error { return nil }
	host.applyParentForeground = func(windowHandle) error { return nil }
	host.syncWindowBounds = func(string) {}
	logger.onWarn = func() { host.windowProc(hwnd, wmNativeShow, 0, run.token) }
	host.windowProc(hwnd, wmNativeHide, 0, run.token)
	if !visible || parentHides != 0 {
		t.Fatalf("nested Show was rolled back by outer Hide: visible=%v parent_hides=%d", visible, parentHides)
	}
}

func TestParentHideReentrantShowMakesOuterHideStale(t *testing.T) {
	host, hwnd, _ := newTransactionalShowHost(t)
	run := host.currentRun()
	visible := true
	reentered := false
	parentHides := 0
	host.applyControllerVisibility = func(bool) error { return nil }
	host.applyParentVisibility = func(_ windowHandle, command int32) error {
		if command == swHide && !reentered {
			reentered = true
			host.windowProc(hwnd, wmNativeShow, 0, run.token)
			return nil
		}
		visible = command != swHide
		if !visible {
			parentHides++
		}
		return nil
	}
	host.queryParentVisible = func(windowHandle) bool { return visible }
	host.applyParentUpdate = func(windowHandle) error { return nil }
	host.applyParentForeground = func(windowHandle) error { return nil }
	host.syncWindowBounds = func(string) {}
	host.windowProc(hwnd, wmNativeHide, 0, run.token)
	if !visible || parentHides != 0 {
		t.Fatalf("synchronous nested Show lost ownership: visible=%v parent_hides=%d", visible, parentHides)
	}
}

func TestRollbackParentHideReentrantShowCancelsOuterRollback(t *testing.T) {
	host, hwnd, _ := newTransactionalShowHost(t)
	run := host.currentRun()
	visible := false
	reentered := false
	destroys := 0
	host.applyControllerVisibility = func(bool) error { return nil }
	host.applyParentVisibility = func(_ windowHandle, command int32) error {
		if command == swHide && !reentered {
			reentered = true
			host.windowProc(hwnd, wmNativeShow, 0, run.token)
			return nil
		}
		visible = command != swHide
		return nil
	}
	host.queryParentVisible = func(windowHandle) bool { return visible }
	updateCalls := 0
	host.applyParentUpdate = func(windowHandle) error {
		updateCalls++
		if updateCalls == 1 {
			return errors.New("outer update failure")
		}
		return nil
	}
	host.applyParentForeground = func(windowHandle) error { return nil }
	host.syncWindowBounds = func(string) {}
	host.destroyNativeWindow = func(windowHandle) { destroys++ }
	host.windowProc(hwnd, wmNativeShow, 0, run.token)
	if !visible || destroys != 0 {
		t.Fatalf("nested Show lost rollback ownership: visible=%v destroys=%d", visible, destroys)
	}
}

func TestVisibilityTerminalRejectsErrorLoggerReentrantShow(t *testing.T) {
	logger := &errorReentrantVisibilityLogger{captureLogger: &captureLogger{}}
	host := New(Config{Logger: logger})
	stubExternalOpen(host)
	const hwnd = windowHandle(0x164)
	run := beginHeadlessLifecycleRun(t, host, hwnd)
	host.browser = &webview2.Browser{}
	var effects, destroys int
	host.applyControllerVisibility = func(bool) error { effects++; return nil }
	host.applyParentVisibility = func(windowHandle, int32) error { effects++; return nil }
	host.destroyNativeWindow = func(windowHandle) { destroys++ }
	nestedResult := uintptr(1)
	logger.onError = func() { nestedResult = host.windowProc(hwnd, wmNativeShow, 0, run.token) }
	host.applyVisibilityTerminalTeardown(hwnd)
	host.applyVisibilityTerminalTeardown(hwnd)
	if nestedResult != 0 || effects != 0 || destroys != 1 {
		t.Fatalf("terminal reentry = result %d effects %d destroys %d", nestedResult, effects, destroys)
	}
}
