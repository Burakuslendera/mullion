//go:build windows

package host

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// TestNavigateFailureUncommitsAndTearsDownBrowser locks the post-Embed error
// path. Once createWebView has committed host.browser, the only releaser of the
// browser's COM references is ShuttingDown from WM_DESTROY - and on the initial
// embed path a Navigate failure returns out of Run before the message loop
// starts, so WM_DESTROY never comes and the browser process leaks with COM
// still referenced past CoUninitialize.
//
// A fresh webview2.Browser has nil COM fields and ShuttingDown tolerates them,
// so this drives the real control flow without a runtime, the same way the
// registerEventsOrTearDown tests do for the in-Embed half. The actual Release
// calls are live-only; the load-bearing headless half is that a Navigate
// failure uncommits host.browser and runs the teardown at all.
func TestNavigateFailureUncommitsAndTearsDownBrowser(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	browser := webview2.New()
	host.browser = browser
	wantErr := errors.New("navigate failed")

	err := host.committedBrowserStepOrTearDown(func() error { return wantErr })

	if !errors.Is(err, wantErr) {
		t.Fatalf("committedBrowserStepOrTearDown err = %v, want %v", err, wantErr)
	}
	if host.browser != nil {
		t.Fatal("a Navigate failure must uncommit host.browser, or ensureWebView reuses a torn-down browser on retry")
	}
	if !browser.IsShuttingDown() {
		t.Fatal("a Navigate failure must tear the browser down, or the browser process and COM references outlive Run")
	}
}

// TestNavigateSuccessKeepsBrowser is the other half: success must not tear
// anything down, or every window would be destroyed at startup.
func TestNavigateSuccessKeepsBrowser(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	browser := webview2.New()
	host.browser = browser

	if err := host.committedBrowserStepOrTearDown(func() error { return nil }); err != nil {
		t.Fatalf("committedBrowserStepOrTearDown err = %v, want nil", err)
	}
	if host.browser != browser {
		t.Fatal("a successful navigation must keep the committed browser")
	}
	if browser.IsShuttingDown() {
		t.Fatal("a successful navigation must not tear the browser down")
	}
}

// Embed pumps the message loop, so ensureWebView can be re-entered from inside
// its own create. The single-flight flag must make the inner call fail without
// running a second embed - two browsers would race for one host.browser commit
// and the loser would leak, browser process and all (issue #23, defect 1).
func TestEnsureWebViewRefusesAReentrantEmbed(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	var outerRuns, innerRuns int
	var innerErr error

	err := host.ensureWebViewWith("initial", func() error {
		outerRuns++
		innerErr = host.ensureWebViewWith("show", func() error {
			innerRuns++
			return nil
		})
		return nil
	})

	if err != nil {
		t.Fatalf("outer ensureWebViewWith err = %v, want nil", err)
	}
	if outerRuns != 1 {
		t.Fatalf("outer create ran %d times, want 1", outerRuns)
	}
	if innerErr == nil {
		t.Fatal("the re-entrant call must fail while an embed is in flight")
	}
	if innerRuns != 0 {
		t.Fatalf("inner create ran %d times, want 0: a second embed leaks a browser", innerRuns)
	}
}

// An already-embedded browser short-circuits before any guard: the post-commit
// show path relies on this returning nil without running create again.
func TestEnsureWebViewReturnsImmediatelyWhenEmbedded(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.browser = webview2.New()

	err := host.ensureWebViewWith("show", func() error {
		t.Error("create must not run when a browser is already embedded")
		return nil
	})
	if err != nil {
		t.Fatalf("ensureWebViewWith with an embedded browser err = %v, want nil", err)
	}
}

func TestEnsureWebViewRejectsReentrantShowWhileCommittedSetupIsInFlight(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	host.browser = webview2.New()
	host.webViewEmbedding = true

	err := host.ensureWebViewWith("show", func() error {
		t.Fatal("re-entrant Show must not use or replace the in-flight browser")
		return nil
	})
	if err == nil {
		t.Fatal("re-entrant Show accepted a committed but unready browser")
	}
	for _, forbidden := range []string{"window visible", "navigate requested", "injected scripts registered", "frontend ready", "host ready"} {
		if strings.Contains(logger.String(), forbidden) {
			t.Fatalf("re-entrant Show leaked %q:\n%s", forbidden, logger.String())
		}
	}
}

// The in-flight flag must clear on both exits, or one failed embed would
// refuse every retry for the life of the host.
func TestEnsureWebViewClearsTheInFlightFlag(t *testing.T) {
	host, _ := newTestHost(t, Config{})

	if err := host.ensureWebViewWith("initial", func() error { return errors.New("embed failed") }); err == nil {
		t.Fatal("a failing create must propagate its error")
	}
	var retried bool
	if err := host.ensureWebViewWith("show", func() error { retried = true; return nil }); err != nil {
		t.Fatalf("retry after a failed embed err = %v, want nil", err)
	}
	if !retried {
		t.Fatal("the retry must run create again: the failure path left the flag set")
	}
}

// A destroyed window has nothing to embed into: create must never run.
func TestEnsureWebViewRefusesAfterDestroy(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.windowDestroyed = true

	err := host.ensureWebViewWith("show", func() error {
		t.Error("create must not run against a destroyed window")
		return nil
	})
	if err == nil {
		t.Fatal("ensureWebView must refuse once the window is destroyed")
	}
}

// TestCommitRefusedAfterMidEmbedDestroy locks defect 2 of issue #23: a
// WM_DESTROY dispatched inside the embed pump skips ShuttingDown because
// host.browser is still nil, so committing the browser afterwards would strand
// it forever - its teardown moment has already passed. The commit must tear it
// down instead.
func TestCommitRefusedAfterMidEmbedDestroy(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	browser := webview2.New()
	host.windowDestroyed = true

	err := host.commitEmbeddedBrowser(browser)

	if err == nil {
		t.Fatal("committing after a mid-embed destroy must fail")
	}
	if host.browser != nil {
		t.Fatal("a browser must not be committed to a destroyed window")
	}
	if !browser.IsShuttingDown() {
		t.Fatal("the uncommitted browser must be torn down, or its COM references and process leak")
	}
}

func TestCommitAssignsTheBrowserOnALiveWindow(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	browser := webview2.New()

	if err := host.commitEmbeddedBrowser(browser); err != nil {
		t.Fatalf("commitEmbeddedBrowser err = %v, want nil", err)
	}
	if host.browser != browser {
		t.Fatal("a live window must receive the embedded browser")
	}
	if browser.IsShuttingDown() {
		t.Fatal("a committed browser must not be torn down")
	}
}

// Every WebView2 event handler recovers its own panics - that recover is what
// keeps a Go panic from unwinding into Chromium's C++ stack and killing the
// process - and reports them through internal/webview2's panic hook. With no
// hook installed the report falls back to a line on os.Stderr, so a panic in a
// navigation or web-message callback never reaches Config.Logger: invisible to
// an embedder who captures logs rather than watching a console, and invisible in
// the log they attach to a bug report. The embed path must install the hook.
//
// The assertion deliberately reads the hook back out of internal/webview2 rather
// than calling the host's reporter directly: the reporter would log either way,
// and the defect being closed here is precisely that nothing ever handed it to
// the package that calls it.
func TestHandlerPanicReachesTheHostLogger(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	t.Cleanup(func() { webview2.SetHandlerPanicHook(nil) })

	if err := host.ensureWebViewWith("initial", func() error { return nil }); err != nil {
		t.Fatalf("ensureWebViewWith err = %v, want nil", err)
	}

	hook := webview2.HandlerPanicHook()
	if hook == nil {
		t.Fatal("the embed path installed no handler panic hook: a recovered handler panic goes to stderr and never reaches Config.Logger")
	}
	// A panic value is whatever the callback panicked with: a message with a
	// developer's path in it, a newline, and a terminal escape are all reachable
	// from here, and none of them may survive into the log line.
	hook("NavigationCompleted",
		"boom\r\n\x1b[2Jfaked from C:\\Users\\jane\\app\\main.go",
		[]byte("goroutine 7 [running]:\nwebview2.(*eventHandler).dispatch(0x1)\n"))

	text := logger.String()
	const wantError = "level=ERROR msg=mullion: webview2 handler recovered from panic, event=NavigationCompleted, reason=boom [2Jfaked from main.go"
	if !strings.Contains(text, wantError) {
		t.Fatalf("the recovered panic did not reach the logger as %q:\n%s", wantError, text)
	}
	if strings.Contains(text, "jane") {
		t.Errorf("the panic message leaked a user path into the log:\n%s", text)
	}
	if strings.Contains(text, "\x1b") {
		t.Errorf("the panic message smuggled a terminal escape into the log:\n%s", text)
	}
	if !strings.Contains(text, "level=DEBUG msg=mullion: webview2 handler panic stack, event=NavigationCompleted") ||
		!strings.Contains(text, "dispatch") {
		t.Errorf("the stack did not reach the logger, so the report cannot lead back to the callback:\n%s", text)
	}
}

// The hook is process-global while the wiring is per-host, so the newest embed
// owns the report path. That is what a second host in one process needs - it
// embeds after the first host's Run returned and its handlers are gone - and it
// is why the reporter reads a published target instead of closing over whichever
// Host happened to embed first.
func TestHandlerPanicFollowsTheMostRecentEmbed(t *testing.T) {
	first, firstLog := newTestHost(t, Config{})
	second, secondLog := newTestHost(t, Config{})
	t.Cleanup(func() { webview2.SetHandlerPanicHook(nil) })

	for _, host := range []*Host{first, second} {
		if err := host.ensureWebViewWith("initial", func() error { return nil }); err != nil {
			t.Fatalf("ensureWebViewWith err = %v, want nil", err)
		}
	}

	hook := webview2.HandlerPanicHook()
	if hook == nil {
		t.Fatal("the embed path installed no handler panic hook")
	}
	hook("WebMessageReceived", "bridge exploded", nil)

	if !strings.Contains(secondLog.String(), "mullion: webview2 handler recovered from panic, event=WebMessageReceived, reason=bridge exploded") {
		t.Errorf("the panic did not reach the most recently embedded host's logger:\n%s", secondLog.String())
	}
	if strings.Contains(firstLog.String(), "recovered from panic") {
		t.Errorf("the panic reached a host that no longer owns the report path:\n%s", firstLog.String())
	}
}

// The watchdog is armed immediately before Navigate, so the failure path must
// disarm it: with the webview torn down, a later "frontend render timeout"
// ERROR would point at a window that no longer exists.
func TestNavigateFailureStopsTheRenderWatchdog(t *testing.T) {
	host, logger := newTestHost(t, Config{RenderTimeout: 20 * time.Millisecond})
	browser := webview2.New()
	host.browser = browser
	host.startRenderWatchdog()

	_ = host.committedBrowserStepOrTearDown(func() error { return errors.New("navigate failed") })
	time.Sleep(60 * time.Millisecond)

	if strings.Contains(logger.String(), "mullion: frontend render timeout") {
		t.Fatal("the render watchdog fired after the failed navigation tore the webview down")
	}
}

func TestFilterFailureCleansUpLogsOnceAndAllowsRetry(t *testing.T) {
	host, logger := newTestHost(t, Config{VirtualHost: "APP_ONE.INTERNAL"})
	wantErr := errors.New("filter failed")
	first := webview2.New()
	var firstPattern string

	err := host.ensureWebViewWith("initial", func() error {
		host.browser = first
		return host.registerAssetFilterOrTearDown(func(pattern string, context webview2.WebResourceContext) error {
			firstPattern = pattern
			if context != webview2.WebResourceContextAll {
				t.Fatalf("filter context = %v, want all", context)
			}
			return wantErr
		})
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("filter failure = %v, want %v", err, wantErr)
	}
	if firstPattern != "https://app_one.internal/*" {
		t.Fatalf("filter pattern = %q", firstPattern)
	}
	if host.browser != nil || !first.IsShuttingDown() {
		t.Fatal("failed filter did not uncommit and shut down the browser")
	}
	logged := logger.String()
	if strings.Contains(logged, wantErr.Error()) {
		t.Fatalf("returned filter failure was also reported by ensureWebViewWith:\n%s", logged)
	}
	for _, falseSuccess := range []string{
		"webresource filter registered",
		"asset serving ready",
		"injected scripts registered",
		"navigate requested",
	} {
		if strings.Contains(logged, falseSuccess) {
			t.Fatalf("filter failure logged a false later stage %q:\n%s", falseSuccess, logged)
		}
	}

	second := webview2.New()
	var retryCalls int
	err = host.ensureWebViewWith("retry", func() error {
		host.browser = second
		return host.registerAssetFilterOrTearDown(func(pattern string, context webview2.WebResourceContext) error {
			retryCalls++
			if pattern != firstPattern || context != webview2.WebResourceContextAll {
				t.Fatalf("retry filter = %q/%v, want %q/all", pattern, context, firstPattern)
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if retryCalls != 1 || host.browser != second || second.IsShuttingDown() {
		t.Fatalf("retry state = calls %d, committed %v, shutdown %v", retryCalls, host.browser == second, second.IsShuttingDown())
	}
	logged = logger.String()
	if strings.Count(logged, "webresource filter registered") != 1 {
		t.Fatalf("retry did not log exactly one successful filter registration:\n%s", logged)
	}
	if strings.Contains(logged, "asset serving ready, source=embedded-fs") {
		t.Fatalf("filter registration alone must not claim startup readiness:\n%s", logged)
	}
}

func TestExternalSourceSkipsEmbeddedFilterWithoutTearingDown(t *testing.T) {
	host, logger := newTestHost(t, Config{URL: testExternalURL, VirtualHost: "invalid/ignored"})
	browser := webview2.New()
	host.browser = browser
	var calls int
	err := host.registerAssetFilterOrTearDown(func(string, webview2.WebResourceContext) error {
		calls++
		return errors.New("must not run")
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || host.browser != browser || browser.IsShuttingDown() {
		t.Fatalf("external filter state = calls %d, committed %v, shutdown %v", calls, host.browser == browser, browser.IsShuttingDown())
	}
	logged := logger.String()
	if !strings.Contains(logged, "asset serving skipped, source=external-url") ||
		strings.Contains(logged, "webresource filter registered") {
		t.Fatalf("external filter logging is false or missing:\n%s", logged)
	}
}

func TestCreateWebViewProductionWiresMandatoryAssetFilterGateInOrder(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "webview_windows.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := make(map[string]*ast.FuncDecl)
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			functions[function.Name.Name] = function
		}
	}
	create := functions["createWebView"]
	startup := functions["startWebViewFirstNavigation"]
	filterGate := functions["registerAssetFilterOrTearDown"]
	if create == nil || startup == nil || filterGate == nil {
		t.Fatal("production createWebView/startup/filter teardown declaration missing")
	}

	collectCalls := func(function *ast.FuncDecl) map[string][]*ast.CallExpr {
		calls := make(map[string][]*ast.CallExpr)
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				if name := webViewASTSelectorPath(call.Fun); name != "" {
					calls[name] = append(calls[name], call)
				}
			}
			return true
		})
		return calls
	}
	createCalls := collectCalls(create)
	one := func(calls map[string][]*ast.CallExpr, function, name string) *ast.CallExpr {
		found := calls[name]
		if len(found) != 1 {
			t.Fatalf("%s %s calls = %d, want exactly one", function, name, len(found))
		}
		return found[0]
	}
	commit := one(createCalls, "createWebView", "host.commitEmbeddedBrowser")
	gate := one(createCalls, "createWebView", "host.registerAssetFilterOrTearDown")
	seam := one(createCalls, "createWebView", "host.startWebViewFirstNavigation")
	if !(commit.Pos() < gate.Pos() && gate.Pos() < seam.Pos()) {
		t.Fatalf("mandatory startup order is commit %s, filter %s, startup seam %s",
			fset.Position(commit.Pos()), fset.Position(gate.Pos()), fset.Position(seam.Pos()))
	}

	startupCalls := collectCalls(startup)
	scriptGate := one(startupCalls, "startWebViewFirstNavigation", "host.registerRequiredDocumentCreatedScripts")
	watchdog := one(startupCalls, "startWebViewFirstNavigation", "startWatchdog")
	var navigate *ast.CallExpr
	for _, step := range startupCalls["host.committedBrowserStepOrTearDown"] {
		if len(step.Args) == 1 && webViewASTSelectorPath(step.Args[0]) == "navigate" {
			navigate = step
		}
	}
	if navigate == nil || !(scriptGate.Pos() < watchdog.Pos() && watchdog.Pos() < navigate.Pos()) {
		t.Fatal("startup seam must keep the script barrier before watchdog and committed Navigate")
	}
	if len(seam.Args) != 5 {
		t.Fatalf("startup seam arguments = %d, want five", len(seam.Args))
	}
	navigation, ok := seam.Args[4].(*ast.FuncLit)
	if !ok || len(collectCalls(&ast.FuncDecl{Body: navigation.Body})["browser.Navigate"]) != 1 {
		t.Fatal("startup seam must receive the production browser Navigate effect")
	}
	if got := len(createCalls["browser.Navigate"]); got != 1 {
		t.Fatalf("createWebView browser.Navigate calls = %d, want one startup-seam effect", got)
	}
	if got := len(createCalls["host.startRenderWatchdog"]); got != 0 {
		t.Fatalf("createWebView direct watchdog calls = %d, want zero", got)
	}
	for _, success := range []string{"asset serving ready, source=embedded-fs", "injected scripts registered", "navigate requested"} {
		for _, call := range createCalls["host.log.Debug"] {
			if len(call.Args) != 1 {
				continue
			}
			if literal, ok := call.Args[0].(*ast.BasicLit); ok && strings.Contains(literal.Value, success) {
				t.Fatalf("createWebView contains pre-barrier success log %q", success)
			}
		}
	}

	if len(gate.Args) != 1 {
		t.Fatalf("filter gate arguments = %d, want registration callback", len(gate.Args))
	}
	callback, ok := gate.Args[0].(*ast.FuncLit)
	if !ok {
		t.Fatal("filter gate no longer receives the production browser registration callback")
	}
	var registrations []*ast.CallExpr
	ast.Inspect(callback.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && webViewASTSelectorPath(call.Fun) == "browser.AddWebResourceRequestedFilter" {
			registrations = append(registrations, call)
		}
		return true
	})
	if len(registrations) != 1 ||
		len(registrations[0].Args) != 2 ||
		webViewASTSelectorPath(registrations[0].Args[0]) != "pattern" ||
		webViewASTSelectorPath(registrations[0].Args[1]) != "context" {
		t.Fatal("production callback must forward the gate's pattern/context to AddWebResourceRequestedFilter")
	}

	filterCalls := collectCalls(filterGate)
	register := filterCalls["register"]
	teardown := filterCalls["host.committedBrowserStepOrTearDown"]
	if len(register) != 1 || len(register[0].Args) != 2 ||
		webViewASTSelectorPath(register[0].Args[0]) != "host.source.filterPattern" ||
		webViewASTSelectorPath(register[0].Args[1]) != "webview2.WebResourceContextAll" {
		t.Fatal("filter gate must register the source-plan pattern for WebResourceContextAll")
	}
	if len(teardown) != 1 || len(teardown[0].Args) != 1 {
		t.Fatal("filter registration must pass through committedBrowserStepOrTearDown")
	}
	insideTeardown := false
	ast.Inspect(teardown[0].Args[0], func(node ast.Node) bool {
		if node == register[0] {
			insideTeardown = true
		}
		return true
	})
	if !insideTeardown {
		t.Fatal("canonical filter registration bypasses the teardown gate")
	}
}

func webViewASTSelectorPath(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.SelectorExpr:
		prefix := webViewASTSelectorPath(expression.X)
		if prefix == "" {
			return ""
		}
		return prefix + "." + expression.Sel.Name
	default:
		return ""
	}
}
