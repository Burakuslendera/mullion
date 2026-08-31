//go:build windows

package webview2

import (
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// iidAddScriptToExecuteOnDocumentCreatedCompletedHandler is from
// Microsoft.Web.WebView2 1.0.4129.50 WebView2.idl. It is a direct IUnknown
// interface, so Invoke occupies literal vtable slot 3.
var iidAddScriptToExecuteOnDocumentCreatedCompletedHandler = windows.GUID{
	Data1: 0xb99369f3, Data2: 0x9b11, Data3: 0x47b5,
	Data4: [8]byte{0xbc, 0x6f, 0x8e, 0x78, 0x95, 0xfc, 0xea, 0x17},
}

// scriptCompletionVtbl is the C ABI of
// ICoreWebView2AddScriptToExecuteOnDocumentCreatedCompletedHandler: IUnknown
// followed by Invoke(this, HRESULT errorCode, LPCWSTR result). result is a
// borrowed UTF-16 script ID, not a COM interface pointer.
type scriptCompletionVtbl struct {
	IUnknownVtbl
	Invoke ComProc
}

type scriptCompletion struct {
	errorCode uintptr
	resultSet bool
}

// scriptCompletionHandler owns exactly one registration's completion. Its
// server must remain first so the address handed to WebView2 starts with the
// vtable word required by COM.
type scriptCompletionHandler struct {
	server   comServer
	this     uintptr
	done     chan struct{}
	required bool

	mu           sync.Mutex
	completion   scriptCompletion
	completed    bool
	published    bool
	abandoned    bool
	duplicate    bool
	onCompletion func(error)
}

func newScriptCompletionHandler() *scriptCompletionHandler {
	return newScriptCompletionHandlerFor(false)
}

func newRequiredScriptCompletionHandler() *scriptCompletionHandler {
	return newScriptCompletionHandlerFor(true)
}

func newScriptCompletionHandlerFor(required bool) *scriptCompletionHandler {
	if !ensureCOMVtables() {
		return nil
	}
	handler := &scriptCompletionHandler{done: make(chan struct{}), required: required}
	handler.this = handler.server.register(
		uintptr(unsafe.Pointer(&scriptCompletionVtable)),
		iidAddScriptToExecuteOnDocumentCreatedCompletedHandler,
		handler,
	)
	return handler
}

func (h *scriptCompletionHandler) release() {
	serverRelease(h.this)
}

// abandon seals the handler before the caller drops its reference. A late or
// duplicate callback then cannot turn a timed-out, cancelled, or completed
// registration back into success.
func (h *scriptCompletionHandler) abandon() {
	h.mu.Lock()
	h.abandoned = true
	h.mu.Unlock()
}

func (h *scriptCompletionHandler) result() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.duplicate {
		return errors.New("webview2: document-created script completion invoked more than once")
	}
	if !h.completed {
		return errors.New("webview2: document-created script completion was not delivered")
	}
	if err := hres(h.completion.errorCode); err != nil {
		return fmt.Errorf("webview2: document-created script registration failed: %w", err)
	}
	if !h.completion.resultSet {
		return errors.New("webview2: document-created script registration reported success without an id")
	}
	return nil
}

func (h *scriptCompletionHandler) publish() bool {
	h.mu.Lock()
	if h.abandoned || h.published {
		h.mu.Unlock()
		return false
	}
	h.published = true
	callback := h.onCompletion
	close(h.done)
	h.mu.Unlock()
	if callback != nil {
		dispatchScriptCompletion(callback, h.result())
	}
	return true
}

// scriptCompletionInvoke is called through literal vtable slot 3. result is
// only checked for the documented script ID; it is never retained or
// treated as IUnknown, because WebView2 owns that LPCWSTR for this call only.
func scriptCompletionInvoke(this, errorCode, result uintptr) uintptr {
	server := serverFor(this)
	if server == nil {
		return eFail
	}
	handler, ok := server.self.(*scriptCompletionHandler)
	if !ok {
		return eFail
	}

	handler.mu.Lock()
	if handler.abandoned {
		handler.mu.Unlock()
		return eFail
	}
	if handler.completed {
		handler.duplicate = true
		handler.mu.Unlock()
		return eFail
	}
	handler.completed = true
	handler.completion = scriptCompletion{errorCode: errorCode, resultSet: result != 0}
	if handler.required && delayRequiredScriptCompletionPublication(handler.publish) {
		handler.mu.Unlock()
		return sOK
	}
	handler.published = true
	callback := handler.onCompletion
	close(handler.done)
	handler.mu.Unlock()
	if callback != nil {
		dispatchScriptCompletion(callback, handler.result())
	}
	return sOK
}

func dispatchScriptCompletion(callback func(error), err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			reportHandlerPanic("document-created script completion", recovered, debug.Stack())
		}
	}()
	callback(err)
}

type scriptCompletionWaiter func(*scriptCompletionHandler) error

// waitForRequiredScriptCompletion is the headless effect seam for the required
// script barrier's cancellation and quit decision. Tests use it to prove the
// cancellation > queued WM_QUIT > completion > timeout precedence without
// entering Win32. Loader and controller creation retain waitFor's established
// completion-first policy.
func waitForRequiredScriptCompletion[T any](done <-chan T, cancelled <-chan struct{}, timeout time.Duration, what string, queuedQuit func() bool, step func() bool, finish func()) (T, error) {
	var zero T
	defer finish()
	const (
		waitPending = iota
		waitCancelled
		waitQuit
		waitCompleted
	)

	cancelledReady := func() bool {
		select {
		case <-cancelled:
			return true
		default:
			return false
		}
	}
	completionReady := func() (T, bool) {
		select {
		case value := <-done:
			return value, true
		default:
			return zero, false
		}
	}
	probe := func() (T, int) {
		if cancelledReady() {
			return zero, waitCancelled
		}
		quit := queuedQuit()
		// The queue drain can dispatch teardown and the completion in one turn.
		// Re-read cancellation before trusting either the quit bit or completion.
		if cancelledReady() {
			return zero, waitCancelled
		}
		if quit {
			return zero, waitQuit
		}
		if value, ready := completionReady(); ready {
			return value, waitCompleted
		}
		return zero, waitPending
	}

	deadline := time.Now().Add(timeout)
	for {
		value, outcome := probe()
		switch outcome {
		case waitCancelled:
			return zero, fmt.Errorf("webview2: cancelled waiting for %s", what)
		case waitQuit:
			return zero, fmt.Errorf("webview2: quit while waiting for %s", what)
		case waitCompleted:
			return value, nil
		}
		if !time.Now().Before(deadline) {
			// Probe the queue again after observing the deadline. Work that became
			// ready at the boundary keeps the same precedence over timeout.
			value, outcome = probe()
			switch outcome {
			case waitCancelled:
				return zero, fmt.Errorf("webview2: cancelled waiting for %s", what)
			case waitQuit:
				return zero, fmt.Errorf("webview2: quit while waiting for %s", what)
			case waitCompleted:
				return value, nil
			}
			return zero, fmt.Errorf("webview2: gave up after %s waiting for %s", timeout, what)
		}
		quit := step()
		if cancelledReady() {
			return zero, fmt.Errorf("webview2: cancelled waiting for %s", what)
		}
		if quit {
			return zero, fmt.Errorf("webview2: quit while waiting for %s", what)
		}
		// Loop through the nonblocking queue probe before accepting a completion
		// delivered by step; a WM_QUIT may have arrived as that step returned.
	}
}

// RegisterDocumentCreatedScripts registers scripts in caller order and waits
// for every documented completion before returning. It is the surface the host
// uses for required first-document scripts; Init remains for optional callers.
func (browser *Browser) RegisterDocumentCreatedScripts(scripts ...string) error {
	return browser.registerDocumentCreatedScriptsWithWait(scripts, browser.waitForScriptCompletion)
}

func (browser *Browser) registerDocumentCreatedScriptsWithWait(scripts []string, wait scriptCompletionWaiter) error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: register document-created scripts before embed")
	}

	handlers := make([]*scriptCompletionHandler, 0, len(scripts))
	defer func() {
		for _, handler := range handlers {
			handler.abandon()
			handler.release()
		}
	}()

	for index, script := range scripts {
		handler := newRequiredScriptCompletionHandler()
		if handler == nil {
			return errors.New("webview2: document-created script handler is unavailable on this architecture")
		}
		handlers = append(handlers, handler)
		if err := core.AddScriptToExecuteOnDocumentCreated(script, unsafe.Pointer(handler)); err != nil {
			return fmt.Errorf("webview2: register document-created script %d: %w", index+1, err)
		}
		if err := wait(handler); err != nil {
			return fmt.Errorf("webview2: wait for document-created script %d: %w", index+1, err)
		}
		if err := handler.result(); err != nil {
			return fmt.Errorf("webview2: document-created script %d: %w", index+1, err)
		}
		// Waiting dispatches re-entrant callbacks. Keep prior handlers live and
		// reject a duplicate before another dependent script can be registered.
		for priorIndex, prior := range handlers {
			if err := prior.result(); err != nil {
				return fmt.Errorf("webview2: document-created script %d: %w", priorIndex+1, err)
			}
		}
	}
	// Waiting for a later script pumps the UI queue, so re-check every prior
	// handler after the final wait. A duplicate that arrived while another
	// required completion was outstanding must still keep Navigate closed.
	for index, handler := range handlers {
		if err := handler.result(); err != nil {
			return fmt.Errorf("webview2: document-created script %d: %w", index+1, err)
		}
	}
	return nil
}

func (browser *Browser) waitForScriptCompletion(handler *scriptCompletionHandler) error {
	var messages pump
	_, err := waitForRequiredScriptCompletion(
		handler.done,
		browser.shutdown,
		DefaultTimeout,
		"document-created script registration",
		func() bool {
			messages.drain()
			return messages.quitSeen
		},
		func() bool {
			messages.step()
			return messages.quitSeen
		},
		messages.finish,
	)
	return err
}

// registerOptionalDocumentCreatedScript starts advisory registration without
// entering the nested completion pump. WebView2 owns the completion handler
// until it calls back or cancels the request; shutdown seals every retained
// optional handler so a late callback cannot revive the torn-down Browser.
func (browser *Browser) registerOptionalDocumentCreatedScript(script string) error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: register document-created scripts before embed")
	}
	handler := newScriptCompletionHandler()
	if handler == nil {
		return errors.New("webview2: document-created script handler is unavailable on this architecture")
	}
	defer handler.release()
	handler.onCompletion = func(err error) {
		browser.mu.Lock()
		delete(browser.optionalScriptHandlers, handler)
		browser.mu.Unlock()
		if err != nil {
			browser.reportWarning(err)
		}
	}
	browser.mu.Lock()
	if browser.shuttingDown {
		browser.mu.Unlock()
		handler.abandon()
		return errors.New("webview2: browser shut down during optional document-created script registration")
	}
	if browser.optionalScriptHandlers == nil {
		browser.optionalScriptHandlers = make(map[*scriptCompletionHandler]struct{})
	}
	browser.optionalScriptHandlers[handler] = struct{}{}
	browser.mu.Unlock()

	if err := core.AddScriptToExecuteOnDocumentCreated(script, unsafe.Pointer(handler)); err != nil {
		browser.mu.Lock()
		delete(browser.optionalScriptHandlers, handler)
		browser.mu.Unlock()
		handler.abandon()
		return err
	}
	return nil
}
