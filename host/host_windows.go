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

	dpiAwarenessErr    error
	renderMu           sync.Mutex
	renderTimer        *time.Timer
	frontendReady      bool
	frontendShellReady bool
	startupMu          sync.Mutex
	startupShowTimer   *time.Timer
	startupShowOnce    sync.Once
	startupTiming      *startupTiming
	diagnostics        *nativeDiagnostics
	sysMenuLast        sysMenuSnapshot
	boundsMu           sync.Mutex
	lastBoundsSyncLog  boundsSyncLogState

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
	// It is a set rather than the single slot it started as because more than
	// one cancel can be outstanding: 0021's live probe watched the runtime start
	// a second navigation of its own after the first ended, so "a top-frame
	// navigation completes before the next starts" is not a rule of the runtime.
	// With one slot the second cancel evicted the first and the evicted
	// completion armed the error surface, tearing the live frontend down into
	// the fallback page - the exact failure the id consumption exists to prevent
	// (issue #73).
	//
	// The set is small and fixed. An id is only ever removed by its completion,
	// and a completion that never comes would otherwise hold a slot forever, so
	// the oldest entry is evicted when a new cancel needs room. Evicting fails
	// in the pre-issue-73 direction, which is understood, and needing to evict
	// at all means four cancels are outstanding at once.
	cancelledNavIDs [cancelledNavSlots]uint64
	// cancelledNavAnonymous counts cancelled navigations the runtime gave no id
	// for. Identity is what the set above matches on, and without it the only
	// thing left is order: an id-less OperationCanceled completion arriving
	// while one of these is outstanding is taken as its cleanup, the same
	// order-based fallback decision 0020 makes for the error surface. Bounded by
	// the same slot count, because nothing else ever removes an entry.
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
		config:          normalised,
		log:             newLogSink(normalised.Logger),
		js:              normalised.jsScripts(),
		dpiAwarenessErr: enablePerMonitorV2DPIAwareness(),
		startupTiming:   newStartupTiming(normalised.StartHidden),
		diagnostics:     newNativeDiagnostics(),
	}
}

// Run creates the window, embeds the WebView and pumps the message loop until
// the window closes. It blocks and locks the calling goroutine to its OS thread.
func (host *Host) Run() (runErr error) {
	// One line, at INFO, before anything can go wrong. A bug report that carries
	// the log then already answers "which build, on what architecture, against
	// which browser runtime" - three questions that otherwise cost a round trip
	// each, and two of which reporters routinely get wrong from memory.
	_, webViewVersion, _ := webview2.FindRuntime()
	host.log.Info(runtimeSummary(webViewVersion, runtime.Version(), runtime.GOARCH))

	host.log.Debug("mullion: ui thread locking")
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if host.dpiAwarenessErr == nil && !alreadyPerMonitorV2DPIAware() {
		// New may have run on a different thread. Its already-PMv2 acceptance
		// samples the New thread, and a thread-level override there cannot
		// vouch for this one - the thread the window is created on. A process
		// that is genuinely PMv2 passes here too (no override in play), so the
		// re-check costs nothing on the normal path; it only turns the exotic
		// override-on-another-thread case back into the fatal error the DPI
		// gate promises. Unreachable below Windows 1703: the enable has
		// already failed there and the error short-circuits this check.
		host.dpiAwarenessErr = errors.New("the Run thread is not per-monitor-v2 dpi aware")
	}

	loopStarted := false
	lastStage := "startup"
	defer func() {
		if !loopStarted && runErr != nil {
			host.log.Error("mullion: message loop pre-start failure, stage=" + logsafe.Message(lastStage) + ", reason=" + logsafe.Reason(runErr))
		}
	}()

	if host.dpiAwarenessErr != nil {
		host.log.Error("mullion: dpi awareness init failed, reason=" + logsafe.Reason(host.dpiAwarenessErr))
		return host.dpiAwarenessErr
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
	// A pre-loop exit must not leave the window behind - and that includes a
	// panic (an OnReady that explodes, say), which a straight-line call on the
	// error return would miss: the unwind would skip it, the class unregister
	// above would fail against the live window, and the next Run would die in
	// RegisterClassEx (issue #48). Registered after the unregister defer so it
	// runs before it (LIFO); gated on loopStarted so a normal loop exit - whose
	// window died through WM_DESTROY - changes nothing.
	defer func() {
		if !loopStarted {
			host.destroyWindowOutsideLoop("pre_loop_failure")
		}
	}()
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
