//go:build windows

package host

import (
	"errors"
	"runtime"
	"sync"
	"time"

	"github.com/Burakuslendera/mullion/internal/webview2"
	"golang.org/x/sys/windows"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

// Host owns one Win32 window and the WebView2 control embedded in it.
//
// Every exported method except Run may be called from any goroutine: they post
// or send a private WM_APP message to the UI thread instead of touching the HWND
// directly, because Win32 window state may only be mutated from the thread that
// created the window.
type Host struct {
	runMu         sync.Mutex
	running       bool
	runGeneration uint64

	config   Config
	log      *logSink
	js       jsScripts
	mu       sync.RWMutex
	hwnd     windowHandle
	instance windowHandle
	wndProc  uintptr
	assets   assetProvider
	browser  *webview2.Browser

	// webViewEmbedding and windowDestroyed guard the in-flight embed. Embed
	// pumps the message loop for up to a minute, so messages dispatched
	// mid-embed re-enter the window procedure while host.browser is still nil:
	// without the first flag a re-entrant ensureWebView would start a second
	// embed and leak whichever browser loses the commit; without the second, a
	// WM_DESTROY dispatched inside the pump would skip ShuttingDown and the
	// browser committed afterwards would never be torn down (issue #23, decision
	// 0016). Both are UI-thread-confined, like host.browser itself.
	webViewEmbedding bool
	windowDestroyed  bool
	quitPending      bool

	dpiAwarenessErr     error
	renderMu            sync.Mutex
	renderTimer         *time.Timer
	frontendReady       bool
	frontendShellReady  bool
	startupMu           sync.Mutex
	startupShowTimer    *time.Timer
	startupShowReleased bool
	startupTiming       *startupTiming
	diagnostics         *nativeDiagnostics
	sysMenuLast         sysMenuSnapshot
	boundsMu            sync.Mutex
	lastBoundsSyncLog   boundsSyncLogState

	// The error-surface admission state (issues #3, #56, #68; decisions/0017,
	// 0021). The runtime reports the empty string as the source of a data:
	// document (issue #56, measured live on 150.0.4078.65 at both the event args
	// and the core), so the fallback error surface cannot be recognised by its
	// source; the host tracks it by navigation state, correlated by the
	// runtime's navigation id where available:
	//
	//   - errorSurfaceActive admits the surface's empty-source web messages. It
	//     arms when the surface is navigated to - before its load completes,
	//     because the injected diagnostics post from document creation - and
	//     disarms when a navigation away from it commits or its own load
	//     genuinely fails.
	//   - errorSurfacePending is set from the decision to navigate to the
	//     surface until its NavigationStarting is claimed; the claim is what
	//     learns the surface's navigation id.
	//   - errorSurfaceNavID is that id (0 = not known), which lets
	//     noteNavigationOutcome attribute completions positively instead of
	//     counting them (issue #68's defect class).
	//   - errorSurfaceLoading marks the surface's own load in flight, and is
	//     also the order-based fallback's window when identity is unavailable.
	//   - errorSurfaceURL is the exact data: URL last navigated to, which the
	//     claim matches NavigationStarting URIs against.
	//
	// All are read and written only on the UI thread (the navigation and
	// web-message callbacks), like host.browser.
	errorSurfaceActive  bool
	errorSurfacePending bool
	errorSurfaceNavID   uint64
	errorSurfaceLoading bool
	errorSurfaceURL     string

	// cancelledNavIDs are the ids of top-level navigations the
	// PinNavigationToOrigin gate cancelled and whose completions have not
	// arrived yet (0 = empty slot). The runtime completes a put_Cancel'd
	// navigation with OperationCanceled, and that completion must not be read as
	// a load failure that arms the error surface - the cancel is deliberate and
	// the current document stays - so the machine treats a matching completion
	// as cleanup (decisions/0023, 0027).
	//
	// It is a set rather than the single slot it started as because nothing
	// stops more than one cancel being outstanding: put_Cancel is issued while
	// NavigationStarting is handled, while the completion that clears the entry
	// is a separately queued event. With one slot the second cancel evicted the
	// first and the evicted completion armed the error surface, tearing the live
	// frontend down into the fallback page - the exact failure the id
	// consumption exists to prevent (issue #73).
	//
	// The set is small and fixed, and the live entries are kept a dense prefix
	// so eviction happens on occupancy rather than on position. An id is removed
	// by its completion, or by the eviction when all four slots are occupied;
	// either way the dropped navigation reverts to the pre-issue-73 behaviour,
	// which is why the eviction is reported.
	cancelledNavIDs [cancelledNavSlots]uint64
	// cancelledNavAnonymous counts cancelled navigations the runtime gave no id
	// for. Identity is what the set above matches on, and without it the only
	// thing left is order: an id-less OperationCanceled completion arriving
	// while one of these is outstanding is taken as its cleanup, the same
	// order-based fallback decision 0020 makes for the error surface. Bounded by
	// the same slot count, and reaching the bound is reported like the other
	// half's eviction, because nothing but a matching completion removes one.
	cancelledNavAnonymous int

	// navStartID is the id of the last top-level navigation the runtime reported
	// starting, and navStartInOrigin whether that navigation targeted the trusted
	// origin. They answer the one question a completion cannot: where its
	// navigation was going, which is what decides whether an aborted one could
	// have failed for real (benignAbort, decisions/0024). One slot is enough
	// because the pair is only ever read for the completion whose id still
	// matches: a completion for any older navigation finds a different id and
	// falls through to the ordinary failure path, which is the safe direction.
	// UI thread only.
	navStartID       uint64
	navStartInOrigin bool

	// openExternal, when set, replaces the ShellExecute call that hands a URL to
	// the user's default browser. Production never sets it; the test host always
	// does. Routing is policy with a side effect at the end of it, and a test
	// that drives the policy would otherwise perform the effect - which is
	// exactly what happened: a gate test aimed at an https URL opened a browser
	// tab on every run of the headless suite, in CI too (issue #76). The seam
	// also makes the routing decision observable, so which target gets handed
	// over is now pinned by a test rather than only by the live checklist.
	openExternal func(uri string)

	// externalOpenSlots bounds the system-browser launches in flight at once
	// (issue #74, decisions/0029). One token is held for the life of one launch
	// goroutine; the channel is buffered to externalOpenLimit and never closed.
	// Created in New rather than in Run, because a launch is reachable from an
	// event handler before Run's own bookkeeping exists and a nil channel would
	// drop every one of them.
	externalOpenSlots chan struct{}
}

// New prepares a host. It does not create a window; Run does that.
//
// Process DPI awareness is applied here rather than in Run, because
// PER_MONITOR_AWARE_V2 must be set before the process owns any HWND - including
// hidden helper windows created by unrelated libraries - and before any WebView2
// child exists. Waiting until Run would let an early tray icon or a message-only
// window pin the process into an unaware context. Any failure is stored and
// reported from Run.
func New(config Config) *Host {
	normalised := config.normalise()
	return &Host{
		config:            normalised,
		log:               newLogSink(normalised.Logger),
		js:                normalised.jsScripts(),
		dpiAwarenessErr:   enablePerMonitorV2DPIAwareness(),
		startupTiming:     newStartupTiming(normalised.StartHidden),
		diagnostics:       newNativeDiagnostics(),
		externalOpenSlots: make(chan struct{}, externalOpenLimit),
	}
}

// beginRun establishes a fresh window session while retaining the immutable
// configuration and the process-lifetime window callback. A Host is reusable
// after Run returns, but concurrent Run calls on the same Host are not.
func (host *Host) beginRun() error {
	host.runMu.Lock()
	defer host.runMu.Unlock()
	if host.running {
		return errors.New("host is already running")
	}
	if host.window() != 0 || host.browser != nil || host.quitPending {
		return errors.New("previous host window session did not tear down")
	}
	host.running = true
	host.runGeneration++

	host.webViewEmbedding = false
	host.windowDestroyed = false
	host.assets = assetProvider{}
	if host.diagnostics != nil {
		host.diagnostics.reset()
	}
	host.sysMenuLast = sysMenuSnapshot{}
	host.errorSurfaceActive = false
	host.errorSurfacePending = false
	host.errorSurfaceNavID = 0
	host.errorSurfaceLoading = false
	host.errorSurfaceURL = ""
	host.cancelledNavIDs = [cancelledNavSlots]uint64{}
	host.cancelledNavAnonymous = 0
	host.navStartID = 0
	host.navStartInOrigin = false

	host.renderMu.Lock()
	if host.renderTimer != nil {
		host.renderTimer.Stop()
		host.renderTimer = nil
	}
	host.frontendReady = false
	host.frontendShellReady = false
	host.renderMu.Unlock()

	host.startupMu.Lock()
	if host.startupShowTimer != nil {
		host.startupShowTimer.Stop()
		host.startupShowTimer = nil
	}
	host.startupShowReleased = false
	host.startupTiming = newStartupTiming(host.config.StartHidden)
	if host.log != nil {
		host.startupTiming.warnBase = host.log.WarnCount()
		host.startupTiming.errorBase = host.log.ErrorCount()
	}
	host.startupMu.Unlock()

	host.boundsMu.Lock()
	host.lastBoundsSyncLog = boundsSyncLogState{}
	host.boundsMu.Unlock()
	return nil
}

func (host *Host) endRun() {
	host.runMu.Lock()
	host.running = false
	host.runMu.Unlock()
}

// Run creates the window, embeds the WebView and pumps the message loop until
// the window closes. It blocks and locks the calling goroutine to its OS thread.
// The same Host may Run again after the prior call returns; concurrent calls on
// one Host return an error.
func (host *Host) Run() error {
	return host.withRunGuard(func() error {
		return continueAfterRuntimeDiscovery(
			webview2.FindRuntime,
			func(webViewVersion string) {
				// One line, at INFO, before anything can go wrong. A bug report
				// then answers build, architecture, and browser runtime without
				// a round trip.
				host.log.Info(runtimeSummary(webViewVersion, runtime.Version(), runtime.GOARCH))
			},
			host.runAfterRuntimeDiscovery,
		)
	})
}

func (host *Host) withRunGuard(run func() error) error {
	if err := host.beginRun(); err != nil {
		return err
	}
	defer host.endRun()
	return run()
}

// continueAfterRuntimeDiscovery makes the architecture gate the boundary before
// COM, class registration, or HWND creation. Missing runtimes continue to the
// normal immediate/deferred embed path; unsupported process ABIs do not.
func continueAfterRuntimeDiscovery(
	find func() (folder string, version string, err error),
	observeVersion func(string),
	proceed func() error,
) error {
	_, version, discoveryErr := find()
	observeVersion(version)
	if err := runtimeStartupError(discoveryErr); err != nil {
		return err
	}
	return proceed()
}

func (host *Host) runAfterRuntimeDiscovery() (runErr error) {
	host.log.Debug("mullion: ui thread locking")
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	dpiErr := host.dpiAwarenessErr
	if dpiErr == nil && !alreadyPerMonitorV2DPIAware() {
		// New may have run on a different thread. Its already-PMv2 acceptance
		// samples the New thread, and a thread-level override there cannot
		// vouch for this one - the thread the window is created on. Keep this
		// Run-thread result session-local: a later sequential Run may own a
		// different thread and must perform its own check.
		dpiErr = errors.New("the Run thread is not per-monitor-v2 dpi aware")
	}

	loopStarted := false
	lastStage := "startup"
	defer func() {
		if !loopStarted && runErr != nil {
			host.log.Error("mullion: message loop pre-start failure, stage=" + logsafe.Message(lastStage) + ", reason=" + logsafe.Reason(runErr))
		}
	}()

	if dpiErr != nil {
		host.log.Error("mullion: dpi awareness init failed, reason=" + logsafe.Reason(dpiErr))
		return dpiErr
	}
	lastStage = "mullion: dpi awareness applied"
	host.log.Debug("mullion: dpi awareness applied, context=per_monitor_v2")

	if err := host.initializeCOM(); err != nil {
		host.log.Error("mullion: com init failed, reason=" + logsafe.Reason(err))
		return err
	}
	defer windows.CoUninitialize()
	lastStage = "mullion: com init"
	host.log.Debug("mullion: com init")

	if err := validateURL(host.config.URL); err != nil {
		host.log.Error("mullion: config url invalid, reason=" + logsafe.Reason(err))
		return err
	}
	// Always logged, both states, so a pasted log shows where the frontend came
	// from without anyone having to ask (see the Config.URL triage note in
	// docs/verification.md).
	host.log.Info(assetSourceSummary(host.config))
	if host.config.URL == "" {
		if host.config.Assets == nil {
			err := errors.New("asset fs unavailable")
			host.log.Error("mullion: asset serving failed, reason=" + logsafe.Reason(err))
			return err
		}
		host.assets = newAssetProvider(host.config.Assets, host.log, host.config.VirtualHost, host.diagnostics)
	}

	host.log.Debug("mullion: window create requested")
	if err := host.createWindow(); err != nil {
		host.log.Error("mullion: hwnd create failed, reason=" + logsafe.Reason(err))
		return err
	}
	defer unregisterWindowClass(host.config.ClassName, host.instance)
	// Every exit after CreateWindowEx owns a final live-window check, including
	// a pre-loop error or panic, a broken GetMessage, an external WM_QUIT, and a
	// panic after the loop starts. Registered after class unregistration so it
	// runs first (LIFO). Ordinary WM_DESTROY clears the handle in the window
	// procedure, so this defer never destroys twice; independent quit ownership
	// still lets it drain a WM_QUIT re-posted by an embed pump.
	defer host.destroyWindowOutsideLoop("run_exit")
	lastStage = "mullion: hwnd created"
	host.log.Debug("mullion: hwnd created")

	if host.config.StartHidden {
		host.log.Debug("mullion: webview deferred, reason=start_hidden")
	} else {
		if err := host.ensureWebView("initial"); err != nil {
			return err
		}
		// No stage is recorded here, and none is in the StartHidden branch
		// either: lastStage names the last stage that completed, and nothing
		// between this point and the loop can return an error for the deferred
		// reporter to read. An embed failure is reported against
		// "mullion: hwnd created", which is the last stage that did complete.
		host.startStartupShowGate()
	}
	if host.config.OnReady != nil {
		host.config.OnReady()
	}
	host.log.Info("mullion: native host ready")
	host.log.Debug("mullion: message loop entering")
	loopStarted = true
	return host.messageLoop()
}

// runtimeStartupError separates an unsupported process ABI, which must stop
// before COM or HWND creation, from ordinary discovery failures handled by the
// existing immediate/deferred embed paths.
func runtimeStartupError(discoveryErr error) error {
	if errors.Is(discoveryErr, webview2.ErrUnsupportedArchitecture) {
		return discoveryErr
	}
	return nil
}

// initializeCOM enters the apartment. An already-initialised apartment is not an
// error: the host may be embedded in a process that set one up first. That case
// still owes a CoUninitialize - a successful CoInitializeEx must be balanced
// whether it entered the apartment or joined it - so the caller uninitialises
// unconditionally on the nil-error path.
func (host *Host) initializeCOM() error {
	err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	if err == nil {
		host.log.Debug("mullion: com initialized")
		return nil
	}
	if errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		host.log.Debug("mullion: com already initialized")
		return nil
	}
	return err
}
