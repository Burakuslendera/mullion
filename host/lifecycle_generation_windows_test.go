//go:build windows

package host

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func beginHeadlessLifecycleRun(t *testing.T, host *Host, hwnd windowHandle) runAdmission {
	t.Helper()
	if err := host.beginRun(); err != nil {
		t.Fatalf("beginRun = %v", err)
	}
	host.mu.Lock()
	host.hwnd = hwnd
	host.mu.Unlock()
	return host.currentRun()
}

func recycleHeadlessLifecycleRun(t *testing.T, host *Host, hwnd windowHandle) runAdmission {
	t.Helper()
	host.mu.Lock()
	host.hwnd = 0
	host.mu.Unlock()
	host.endRun()
	return beginHeadlessLifecycleRun(t, host, hwnd)
}

type reentrantHostLogger struct {
	host         *Host
	once         atomic.Bool
	entered      chan struct{}
	allowReenter chan struct{}
}

func (logger *reentrantHostLogger) Debug(string) {
	if logger.once.CompareAndSwap(false, true) {
		close(logger.entered)
		<-logger.allowReenter
		logger.host.Quit()
	}
}
func (*reentrantHostLogger) Info(string)  {}
func (*reentrantHostLogger) Warn(string)  {}
func (*reentrantHostLogger) Error(string) {}

func TestLoggerMayReenterHostMethodWhileTeardownWaits(t *testing.T) {
	logger := &reentrantHostLogger{
		entered:      make(chan struct{}),
		allowReenter: make(chan struct{}),
	}
	host := New(Config{Logger: logger})
	logger.host = host
	const hwnd = windowHandle(0x3a3a)
	beginHeadlessLifecycleRun(t, host, hwnd)

	var posts atomic.Int32
	host.postNativeCommand = func(windowHandle, uint32, uintptr, uintptr) error {
		posts.Add(1)
		return nil
	}
	hideDone := make(chan struct{})
	go func() {
		host.Hide()
		close(hideDone)
	}()
	<-logger.entered
	endDone := make(chan struct{})
	go func() {
		host.endRun()
		close(endDone)
	}()
	host.runMu.Lock()
	for !host.runEnding {
		host.runCond.Wait()
	}
	host.runMu.Unlock()
	close(logger.allowReenter)

	select {
	case <-hideDone:
	case <-time.After(time.Second):
		t.Fatal("Logger re-entry deadlocked with teardown waiting for the outer method")
	}
	select {
	case <-endDone:
	case <-time.After(time.Second):
		t.Fatal("teardown did not finish after Logger-reentrant Host methods returned")
	}
	if posts.Load() != 2 {
		t.Fatalf("Hide plus Logger-reentrant Quit posted %d commands, want 2", posts.Load())
	}
}

func TestBeginRunRejectsConcurrentCallDuringTeardown(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	beginHeadlessLifecycleRun(t, host, windowHandle(0x3b3b))

	// Hold one admitted API call so endRun enters its draining state without
	// completing. A Run call made now is concurrent with the first Run and must
	// reject immediately, not wait and become a sequential reuse.
	host.enterRun()
	endDone := make(chan struct{})
	go func() {
		host.endRun()
		close(endDone)
	}()
	host.runMu.Lock()
	for !host.runEnding {
		host.runCond.Wait()
	}
	host.runMu.Unlock()

	beginDone := make(chan error, 1)
	go func() {
		beginDone <- host.beginRun()
	}()
	var beginErr error
	select {
	case beginErr = <-beginDone:
	case <-time.After(time.Second):
		host.leaveRun()
		<-endDone
		t.Fatal("concurrent beginRun waited behind teardown instead of rejecting")
	}
	if beginErr == nil || !strings.Contains(beginErr.Error(), "already running") {
		host.leaveRun()
		<-endDone
		t.Fatalf("concurrent beginRun error = %v, want already running", beginErr)
	}
	host.leaveRun()
	select {
	case <-endDone:
	case <-time.After(time.Second):
		t.Fatal("teardown did not finish after the held API call left")
	}
}

func TestRunTokensAreProcessGlobalAcrossHosts(t *testing.T) {
	first, _ := newTestHost(t, Config{})
	second, _ := newTestHost(t, Config{})
	const recycled = windowHandle(0x4a4a)
	firstRun := beginHeadlessLifecycleRun(t, first, recycled)
	first.mu.Lock()
	first.hwnd = 0
	first.mu.Unlock()
	first.endRun()
	secondRun := beginHeadlessLifecycleRun(t, second, recycled)

	if firstRun.token == 0 || secondRun.token == 0 || firstRun.token == secondRun.token {
		t.Fatalf("process-global tokens = (%#x, %#x), want distinct non-zero identities", firstRun.token, secondRun.token)
	}
	var applied int
	second.applyNativeCommand = func(windowHandle, uint32, uintptr) uintptr {
		applied++
		return 0
	}
	second.windowProc(recycled, wmNativeQuit, 0, firstRun.token)
	if applied != 0 {
		t.Fatal("second Host accepted first Host's stale command after HWND reuse")
	}
	second.windowProc(recycled+1, wmNativeQuit, 0, secondRun.token)
	if applied != 0 {
		t.Fatal("second Host accepted its active token for a foreign HWND")
	}
	second.windowProc(recycled, wmNativeQuit, 0, secondRun.token)
	if applied != 1 {
		t.Fatal("second Host rejected its own active command")
	}
}

// Private-command identities travel through LPARAM as uintptr values. At counter
// wrap, a live first-session value must stay reserved; after its owner ends, the
// same value may be issued again without weakening the collision guard.
func TestNativeRunTokenRegistrySkipsLiveTokenAtUintptrWrap(t *testing.T) {
	registry := newNativeRunTokenRegistry()
	live := registry.reserve()
	if live != 1 {
		t.Fatalf("first native Run token = %#x, want 1", live)
	}

	registry.next = ^uintptr(0)
	next := registry.reserve()
	if next == 0 || next == live {
		t.Fatalf("wrapped native Run token = %#x, want a non-zero value distinct from live %#x", next, live)
	}

	registry.release(live)
	registry.next = ^uintptr(0)
	if reused := registry.reserve(); reused != live {
		t.Fatalf("released native Run token = %#x after wrap, want %#x", reused, live)
	}
}

func TestEndRunReleasesActiveNativeRunToken(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	run := beginHeadlessLifecycleRun(t, host, windowHandle(0x4a4b))

	sharedNativeRunTokens.mu.Lock()
	_, liveBeforeEnd := sharedNativeRunTokens.active[run.token]
	sharedNativeRunTokens.mu.Unlock()
	if !liveBeforeEnd {
		t.Fatal("beginRun did not reserve its active native Run token")
	}

	host.mu.Lock()
	host.hwnd = 0
	host.mu.Unlock()
	host.endRun()

	sharedNativeRunTokens.mu.Lock()
	_, liveAfterEnd := sharedNativeRunTokens.active[run.token]
	sharedNativeRunTokens.mu.Unlock()
	if liveAfterEnd {
		t.Fatal("endRun retained a native Run token after its session ended")
	}
}

func privateCommandPayload(message uint32) uintptr {
	if message == wmNativeStartResize {
		return htLeft
	}
	return 7
}

func TestPrivateCommandsRejectOldRunTokenAfterIdenticalHWNDReuse(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	const hwnd = windowHandle(0x5a5a)
	oldRun := beginHeadlessLifecycleRun(t, host, hwnd)
	newRun := recycleHeadlessLifecycleRun(t, host, hwnd)

	commands := []uint32{
		wmNativeShow,
		wmNativeHide,
		wmNativeQuit,
		wmNativeMinimize,
		wmNativeMaxToggle,
		wmNativeStartDrag,
		wmNativeStartResize,
		wmNativeSyncBounds,
		wmNativeSetTitle,
	}
	applied := make(map[uint32]int)
	host.applyNativeCommand = func(gotHWND windowHandle, message uint32, _ uintptr) uintptr {
		if gotHWND != hwnd {
			t.Fatalf("applied command HWND = %#x, want %#x", gotHWND, hwnd)
		}
		applied[message]++
		if message == wmNativeShow {
			return 1
		}
		return 0
	}

	before := logger.String()
	for _, message := range commands {
		host.windowProc(hwnd, message, privateCommandPayload(message), oldRun.token)
	}
	if len(applied) != 0 {
		t.Fatalf("old Run commands mutated recycled HWND: %#v", applied)
	}
	if after := logger.String(); after != before {
		t.Fatalf("old Run commands logged against recycled HWND:\n%s", after)
	}
	for _, message := range commands {
		host.windowProc(hwnd, message, privateCommandPayload(message), newRun.token)
	}
	for _, message := range commands {
		if applied[message] != 1 {
			t.Errorf("active command %#x applied %d times, want 1", message, applied[message])
		}
	}
}

// The active-Run token/HWND gate runs first, then resize payload validation
// rejects every non-edge value before the operation seam. A malformed private
// message therefore cannot release capture or reach DefWindowProc's non-client
// click path.
func TestResizeCommandRejectsInvalidPayloadBeforeOperationSeam(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	const hwnd = windowHandle(0x5a5b)
	run := beginHeadlessLifecycleRun(t, host, hwnd)
	var applied []uintptr
	host.applyNativeCommand = func(_ windowHandle, message uint32, wParam uintptr) uintptr {
		if message != wmNativeStartResize {
			t.Fatalf("operation seam received message %#x, want resize", message)
		}
		applied = append(applied, wParam)
		return 0
	}

	invalid := []uintptr{0, htClient, htClose, ^uintptr(0)}
	if uint64(^uintptr(0)) > uint64(0xffffffff) {
		high := uint64(htLeft) | uint64(1)<<32
		invalid = append(invalid, uintptr(high))
	}
	for _, payload := range invalid {
		host.windowProc(hwnd, wmNativeStartResize, payload, run.token)
	}
	if len(applied) != 0 {
		t.Fatalf("invalid resize payloads reached operation seam: %#v", applied)
	}
	if count := strings.Count(logger.String(), "mullion: resize rejected, reason=invalid hit"); count != len(invalid) {
		t.Fatalf("invalid resize rejection logs = %d, want %d:\n%s", count, len(invalid), logger.String())
	}

	host.windowProc(hwnd, wmNativeStartResize, htLeft, run.token)
	if len(applied) != 1 || applied[0] != htLeft {
		t.Fatalf("valid resize payload did not reach operation seam unchanged: %#v", applied)
	}
}

func TestStartupShowApplicationFailureRestoresFallbackAndRetries(t *testing.T) {
	host, _ := newTestHost(t, Config{ShowTimeout: time.Hour})
	const hwnd = windowHandle(0x6161)
	run := beginHeadlessLifecycleRun(t, host, hwnd)

	var posts int
	host.postNativeCommand = func(gotHWND windowHandle, message uint32, wParam, token uintptr) error {
		posts++
		if gotHWND != hwnd || message != wmNativeShow || token != run.token {
			t.Fatalf("show post = (%#x, %#x, %#x), want (%#x, %#x, %#x)", gotHWND, message, token, hwnd, wmNativeShow, run.token)
		}
		return nil
	}
	host.startStartupShowGate()
	host.requestStartupShow("frontend_shell_ready")
	if posts != 1 {
		t.Fatalf("initial show posts = %d, want 1", posts)
	}

	// The queued command reached the production dispatch seam, but embedding
	// rejected the application. The fallback must become live again.
	host.applyNativeCommand = func(windowHandle, uint32, uintptr) uintptr { return 0 }
	host.windowProc(hwnd, wmNativeShow, 0, run.token)
	host.startupMu.Lock()
	retryTimer := host.startupShowTimer
	released := host.startupShowReleased
	host.startupMu.Unlock()
	if released || retryTimer == nil {
		t.Fatalf("failed application left gate = (released=%v timer=%v), want open/armed", released, retryTimer != nil)
	}

	retryTimer.Stop()
	host.applyNativeCommand = func(windowHandle, uint32, uintptr) uintptr { return 1 }
	host.fireStartupShowGate(retryTimer, run)
	if posts != 2 {
		t.Fatalf("retry show posts = %d, want 2 total", posts)
	}
	host.startupMu.Lock()
	released = host.startupShowReleased
	retained := host.startupShowTimer
	host.startupMu.Unlock()
	if !released || retained != nil {
		t.Fatalf("successful retry left gate = (released=%v timer=%v), want released/detached", released, retained != nil)
	}
}

func TestOldRunTimersDeferredPostsAndWorkerWarningsStayOutOfNextRun(t *testing.T) {
	host, logger := newTestHost(t, Config{ShowTimeout: time.Hour, RenderTimeout: time.Hour})
	const hwnd = windowHandle(0x7171)
	oldRun := beginHeadlessLifecycleRun(t, host, hwnd)
	host.startStartupShowGate()
	host.startRenderWatchdog()
	host.startupMu.Lock()
	showTimer := host.startupShowTimer
	host.startupMu.Unlock()
	host.renderMu.Lock()
	renderTimer := host.renderTimer
	renderGeneration := host.renderGeneration
	host.renderMu.Unlock()
	if showTimer == nil || renderTimer == nil {
		t.Fatal("originating Run did not arm both timers")
	}
	showTimer.Stop()
	renderTimer.Stop()

	recycleHeadlessLifecycleRun(t, host, hwnd)
	var posts atomic.Int32
	host.postNativeCommand = func(windowHandle, uint32, uintptr, uintptr) error {
		posts.Add(1)
		return errors.New("must not be reported")
	}
	before := logger.String()
	host.fireStartupShowGate(showTimer, oldRun)
	host.fireRenderWatchdog(renderGeneration, oldRun)
	host.fireDeferredBoundsSync(oldRun, boundsSyncWParamDeferredMaximize)
	host.warnForRun(oldRun, "mullion: stale worker warning")

	if got := posts.Load(); got != 0 {
		t.Fatalf("old Run async work posted %d commands into the recycled HWND", got)
	}
	if after := logger.String(); after != before || strings.Contains(after, "stale worker") || strings.Contains(after, "must not be reported") {
		t.Fatalf("old Run async work logged into the next Run:\n%s", after)
	}
}

func TestReadinessAdmittedBeforeTeardownCompletesInsideOriginatingRun(t *testing.T) {
	host, _ := newTestHost(t, Config{ShowTimeout: time.Hour})
	const hwnd = windowHandle(0x8181)
	run := beginHeadlessLifecycleRun(t, host, hwnd)
	host.startStartupShowGate()

	firstPost := make(chan struct{})
	releaseFirstPost := make(chan struct{})
	var postCount atomic.Int32
	var endReturned atomic.Bool
	posted := make(chan struct{}, 2)
	host.postNativeCommand = func(gotHWND windowHandle, message uint32, _ uintptr, token uintptr) error {
		if gotHWND != hwnd || token != run.token {
			t.Errorf("readiness post escaped originating Run: hwnd=%#x token=%#x", gotHWND, token)
		}
		count := postCount.Add(1)
		if count == 1 {
			if message != wmNativeSyncBounds {
				t.Errorf("first readiness post = %#x, want bounds sync", message)
			}
			close(firstPost)
			<-releaseFirstPost
		} else if message != wmNativeShow {
			t.Errorf("second readiness post = %#x, want show", message)
		}
		if endReturned.Load() {
			t.Error("teardown returned before already-admitted readiness finished")
		}
		posted <- struct{}{}
		return nil
	}

	readyDone := make(chan struct{})
	go func() {
		host.MarkFrontendShellReady()
		close(readyDone)
	}()
	<-firstPost
	endDone := make(chan struct{})
	go func() {
		host.endRun()
		endReturned.Store(true)
		close(endDone)
	}()
	close(releaseFirstPost)
	<-readyDone
	<-endDone
	<-posted
	<-posted
	if got := postCount.Load(); got != 2 {
		t.Fatalf("already-admitted readiness posted %d effects, want bounds then show", got)
	}
}

func TestIsMaximisedPinsHWNDOwnershipUntilQueryReturns(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	const hwnd = windowHandle(0x9191)
	beginHeadlessLifecycleRun(t, host, hwnd)

	queryEntered := make(chan struct{})
	releaseQuery := make(chan struct{})
	host.queryNativeMaximised = func(got windowHandle) bool {
		if got != hwnd {
			t.Errorf("query HWND = %#x, want %#x", got, hwnd)
		}
		close(queryEntered)
		<-releaseQuery
		return true
	}
	queryDone := make(chan bool)
	go func() { queryDone <- host.IsMaximised() }()
	<-queryEntered

	destroyDone := make(chan struct{})
	go func() {
		host.beginWindowDestroy(hwnd)
		close(destroyDone)
	}()
	close(releaseQuery)
	if !<-queryDone {
		t.Fatal("pinned maximised query result was lost")
	}
	<-destroyDone
	if host.window() != 0 {
		t.Fatal("WM_DESTROY did not clear HWND after pinned query released it")
	}
}

func TestExportedCommandsCarryEntryRunTokenAndPreserveWParamPayloads(t *testing.T) {
	host, _ := newTestHost(t, Config{ShowTimeout: time.Hour})
	const hwnd = windowHandle(0xa1a1)
	run := beginHeadlessLifecycleRun(t, host, hwnd)

	type command struct {
		message uint32
		wParam  uintptr
		token   uintptr
		hwnd    windowHandle
	}
	var commands []command
	record := func(gotHWND windowHandle, message uint32, wParam, token uintptr) {
		commands = append(commands, command{message, wParam, token, gotHWND})
	}
	host.postNativeCommand = func(gotHWND windowHandle, message uint32, wParam, token uintptr) error {
		record(gotHWND, message, wParam, token)
		return nil
	}
	host.sendNativeCommand = func(gotHWND windowHandle, message uint32, wParam, token uintptr) (uintptr, error) {
		record(gotHWND, message, wParam, token)
		return 1, nil
	}

	host.startStartupShowGate()
	if err := host.Show(); err != nil {
		t.Fatalf("Show = %v", err)
	}
	host.Hide()
	host.Quit()
	host.Minimise()
	host.ToggleMaximise()
	host.StartDrag()
	host.StartResize("left")
	host.SetTitle("session title")
	host.MarkFrontendShellReady()
	host.MarkFrontendReady()

	counts := make(map[uint32]int)
	var resizePayload uintptr
	var boundsPayloads []uintptr
	var titlePayload uintptr
	for _, got := range commands {
		if got.hwnd != hwnd || got.token != run.token {
			t.Errorf("command %#x target = (hwnd=%#x token=%#x), want (%#x, %#x)", got.message, got.hwnd, got.token, hwnd, run.token)
		}
		counts[got.message]++
		switch got.message {
		case wmNativeStartResize:
			resizePayload = got.wParam
		case wmNativeSyncBounds:
			boundsPayloads = append(boundsPayloads, got.wParam)
		case wmNativeSetTitle:
			titlePayload = got.wParam
		default:
			if got.wParam != 0 {
				t.Errorf("command %#x changed zero wParam to %#x", got.message, got.wParam)
			}
		}
	}
	wantCounts := map[uint32]int{
		wmNativeShow:        2,
		wmNativeHide:        1,
		wmNativeQuit:        1,
		wmNativeMinimize:    1,
		wmNativeMaxToggle:   1,
		wmNativeStartDrag:   1,
		wmNativeStartResize: 1,
		wmNativeSyncBounds:  2,
		wmNativeSetTitle:    1,
	}
	for message, want := range wantCounts {
		if counts[message] != want {
			t.Errorf("command %#x count = %d, want %d", message, counts[message], want)
		}
	}
	if resizePayload != uintptr(htLeft) {
		t.Errorf("resize wParam = %#x, want HTLEFT %#x", resizePayload, htLeft)
	}
	if len(boundsPayloads) != 2 ||
		boundsPayloads[0] != boundsSyncWParamFrontendShellReady ||
		boundsPayloads[1] != boundsSyncWParamFrontendReady {
		t.Errorf("bounds wParams = %#v, want shell-ready then ready", boundsPayloads)
	}
	if titlePayload == 0 {
		t.Fatal("SetTitle did not carry its call-lifetime UTF-16 payload in wParam")
	}
}
