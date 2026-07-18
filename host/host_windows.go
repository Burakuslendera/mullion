//go:build windows

package host

import (
	"errors"
	"runtime"
	"sync"
	"time"
	"unsafe"

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

	dpiAwarenessErr   error
	renderMu          sync.Mutex
	renderTimer       *time.Timer
	frontendReady     bool
	startupMu         sync.Mutex
	startupShowTimer  *time.Timer
	startupShowOnce   sync.Once
	startupTiming     *startupTiming
	diagnostics       *nativeDiagnostics
	sysMenuLast       sysMenuSnapshot
	boundsMu          sync.Mutex
	lastBoundsSyncLog boundsSyncLogState

	// errorPageShown guards NavigationCompletedCallback from re-navigating to the
	// fallback error surface in a loop. It is read and written only on the UI
	// thread (the navigation-completed callback), so it needs no lock. See issue #3.
	errorPageShown bool

	// errorSurfaceActive and errorSurfaceLoading admit the fallback error
	// surface's own web messages. The runtime reports the empty string as the
	// source of a data: document (issue #56, measured live on 150.0.4078.65 at
	// both the event args and the core), so the surface cannot be recognised by
	// its source and the host tracks it by navigation state instead:
	// errorSurfaceActive arms when the surface is navigated to - before its load
	// completes, because the injected diagnostics post from document creation -
	// and disarms when a navigation away from it succeeds. errorSurfaceLoading
	// marks the surface's own load in flight so its success completion is not
	// mistaken for that departure. Both are read and written only on the UI
	// thread (the navigation-completed and web-message callbacks), like
	// errorPageShown and host.browser.
	errorSurfaceActive  bool
	errorSurfaceLoading bool
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

	uninitializeCOM, err := host.initializeCOM()
	if err != nil {
		host.log.Error("mullion: com init failed, reason=" + logsafe.Reason(err))
		return err
	}
	if uninitializeCOM {
		defer windows.CoUninitialize()
	}
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
			host.destroyWindowBeforeLoop()
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
		lastStage = "mullion: webview2 embedded"
		host.startStartupShowGate()
	}
	if host.config.OnReady != nil {
		host.config.OnReady()
	}
	host.log.Info("mullion: native host ready")
	host.log.Debug("mullion: message loop entering")
	loopStarted = true
	runErr = host.messageLoop()
	return runErr
}

// initializeCOM reports whether the caller owns the COM apartment. An
// already-initialised apartment is not an error: the host may be embedded in a
// process that set one up first.
func (host *Host) initializeCOM() (bool, error) {
	err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	if err == nil {
		host.log.Debug("mullion: com initialized")
		return true, nil
	}
	if errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		host.log.Debug("mullion: com already initialized")
		return true, nil
	}
	return false, err
}

func (host *Host) createWindow() error {
	host.log.Debug("mullion: module handle requested")
	instance, err := getModuleHandle()
	if err != nil {
		return err
	}
	host.instance = instance
	host.wndProc = newWindowCallback(host.windowProc, host.reportWindowProcPanic)
	host.log.Debug("mullion: win32 class/window create requested")
	hwnd, err := host.createWin32Window(host.config.ClassName, host.config.Title, instance, host.wndProc, host.config.Width, host.config.Height)
	if err != nil {
		return err
	}
	host.mu.Lock()
	host.hwnd = hwnd
	host.mu.Unlock()
	return nil
}

func (host *Host) messageLoop() error {
	var message msg
	for {
		result, _, err := procGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		switch int32(result) {
		case -1:
			host.log.Error("mullion: message loop failed, reason=" + logsafe.Reason(err))
			return syscallError(err)
		case 0:
			host.log.Debug("mullion: message loop exited")
			return nil
		default:
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
		}
	}
}

func (host *Host) window() windowHandle {
	host.mu.RLock()
	defer host.mu.RUnlock()
	return host.hwnd
}

// destroyWindowBeforeLoop tears the HWND down when Run fails after createWindow
// but before the message loop starts.
//
// Without it the window outlives Run invisibly: the deferred
// unregisterWindowClass fails with ERROR_CLASS_HAS_WINDOWS against the live
// window, and a second Run in the same process cannot register the class again
// (issue #48). DestroyWindow dispatches WM_DESTROY synchronously on this
// thread - the teardown case runs (host.browser is nil or already torn down on
// every path that reaches here) and posts WM_QUIT. With no loop ever starting,
// that WM_QUIT would sit in the thread queue and poison the next message loop
// on this thread - a later Run would read it first and exit immediately, a
// silent one-shot failure - so the quit is drained right after the destroy.
// The stored handle is cleared last, so a stray exported call afterwards fails
// the zero-handle guard instead of posting to a recycled HWND.
func (host *Host) destroyWindowBeforeLoop() {
	hwnd := host.window()
	if hwnd == 0 {
		return
	}
	host.log.Debug("mullion: pre-loop window teardown")
	if host.windowDestroyed {
		// A Quit dispatched inside the embed pump already destroyed the real
		// window: the stored handle is stale and is not fed back to
		// DestroyWindow, on the off-chance the value was recycled. Only the
		// drain is still owed - the pump re-posts the quit it swallowed
		// mid-wait, so it is pending right now.
		drainThreadQuitMessage()
	} else {
		teardownBeforeLoop(
			func() { procDestroyWindow.Call(uintptr(hwnd)) },
			drainThreadQuitMessage,
		)
	}
	host.mu.Lock()
	host.hwnd = 0
	host.mu.Unlock()
}

// teardownBeforeLoop orders the two halves of the pre-loop teardown: the
// destroy posts the WM_QUIT (via the window procedure's WM_DESTROY case), so
// the drain must run after it - draining first would remove nothing and leave
// the poison behind. Extracted so the ordering contract is unit-testable
// without a window.
func teardownBeforeLoop(destroy, drain func()) {
	destroy()
	drain()
}

// drainThreadQuitMessage removes any pending WM_QUIT from the calling thread's
// queue. WM_QUIT is a thread message, not a window message, so it survives the
// window's destruction and would be the first thing the next GetMessage on
// this thread returns.
func drainThreadQuitMessage() {
	var message msg
	for {
		got, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, wmQuit, wmQuit, pmRemove)
		if got == 0 {
			return
		}
	}
}
