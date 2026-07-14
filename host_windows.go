//go:build windows

package mullion

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

	if host.config.Assets == nil {
		err := errors.New("asset fs unavailable")
		host.log.Error("mullion: asset serving failed, reason=" + logsafe.Reason(err))
		return err
	}
	host.assets = newAssetProvider(host.config.Assets, host.log, host.config.VirtualHost, host.diagnostics)

	host.log.Debug("mullion: window create requested")
	if err := host.createWindow(); err != nil {
		host.log.Error("mullion: hwnd create failed, reason=" + logsafe.Reason(err))
		return err
	}
	defer unregisterWindowClass(host.config.ClassName, host.instance)
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
	host.wndProc = newWindowCallback(host.windowProc)
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
