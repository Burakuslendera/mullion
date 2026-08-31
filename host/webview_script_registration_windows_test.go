//go:build windows

package host

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

func TestRequiredDocumentCreatedScriptBarrierFailsClosedAndAllowsRetry(t *testing.T) {
	host, logger := newTestHost(t, Config{VirtualHost: "APP_ONE.INTERNAL"})
	first := webview2.New()
	host.browser = first
	failure := errors.New("completion failed")
	var calls [][]string

	err := host.registerRequiredDocumentCreatedScripts(first, func(scripts ...string) error {
		calls = append(calls, append([]string(nil), scripts...))
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("registration error = %v, want %v", err, failure)
	}
	if got, want := strings.Join(calls[0], ","), strings.Join([]string{host.js.bridge, host.js.diagnostics, host.js.drag, host.js.resize}, ","); got != want {
		t.Fatalf("required script initiation order = %q, want %q", got, want)
	}
	if host.browser != nil || !first.IsShuttingDown() {
		t.Fatalf("failed barrier left browser committed=%v shuttingDown=%v", host.browser == first, first.IsShuttingDown())
	}
	for _, forbidden := range []string{"injected scripts registered", "asset serving ready", "navigate requested", "frontend ready", "host ready"} {
		if strings.Contains(logger.String(), forbidden) {
			t.Fatalf("failed barrier logged forbidden success %q:\n%s", forbidden, logger.String())
		}
	}

	second := webview2.New()
	host.browser = second
	if err := host.registerRequiredDocumentCreatedScripts(second, func(scripts ...string) error { return nil }); err != nil {
		t.Fatalf("sequential retry: %v", err)
	}
	if host.browser != second || second.IsShuttingDown() {
		t.Fatalf("retry state committed=%v shuttingDown=%v", host.browser == second, second.IsShuttingDown())
	}
}

func TestRequiredDocumentCreatedScriptBarrierRejectsTeardownDuringCompletion(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	browser := webview2.New()
	host.browser = browser

	err := host.registerRequiredDocumentCreatedScripts(browser, func(...string) error {
		host.windowDestroyed = true
		host.browser = nil
		browser.ShuttingDown()
		return nil
	})
	if err == nil {
		t.Fatal("teardown during completion returned nil")
	}
	if !browser.IsShuttingDown() || host.browser != nil {
		t.Fatalf("teardown state shuttingDown=%v committed=%v", browser.IsShuttingDown(), host.browser == browser)
	}
	if strings.Contains(logger.String(), "injected scripts registered") || strings.Contains(logger.String(), "navigate requested") {
		t.Fatalf("teardown logged startup success:\n%s", logger.String())
	}
}

// TestProductionStartupSeamFailsClosedAndAllowsRetry drives the exact
// post-Embed production seam with effects that count every operation it is
// forbidden to perform after a required registration failure.
func TestProductionStartupSeamFailsClosedAndAllowsRetry(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	first := webview2.New()
	host.browser = first
	var registration, tabStrip, watchdog, navigation int
	failure := errors.New("completion failed")

	err := host.startWebViewFirstNavigation(
		first,
		func(...string) error { registration++; return failure },
		func(*webview2.Browser) error { tabStrip++; return nil },
		func() { watchdog++ },
		func() error { navigation++; return nil },
	)
	if !errors.Is(err, failure) {
		t.Fatalf("startup error = %v, want %v", err, failure)
	}
	if registration != 1 || tabStrip != 0 || watchdog != 0 || navigation != 0 {
		t.Fatalf("failed startup effects register=%d tab=%d watchdog=%d navigate=%d, want 1,0,0,0", registration, tabStrip, watchdog, navigation)
	}
	if host.browser != nil || !first.IsShuttingDown() {
		t.Fatalf("failed startup left browser committed=%v shuttingDown=%v", host.browser == first, first.IsShuttingDown())
	}
	for _, forbidden := range []string{"asset serving ready", "injected scripts registered", "navigate requested", "frontend ready", "host ready"} {
		if strings.Contains(logger.String(), forbidden) {
			t.Fatalf("failed production seam logged %q:\n%s", forbidden, logger.String())
		}
	}

	second := webview2.New()
	host.browser = second
	if err := host.startWebViewFirstNavigation(
		second,
		func(...string) error { return nil },
		func(*webview2.Browser) error { return nil },
		func() { watchdog++ },
		func() error { navigation++; return nil },
	); err != nil {
		t.Fatalf("sequential retry: %v", err)
	}
	if host.browser != second || navigation != 1 {
		t.Fatalf("retry committed=%v navigation=%d, want true and 1", host.browser == second, navigation)
	}
}

func TestProductionStartupSeamSuppressesReadinessAfterOptionalLifecycleFailure(t *testing.T) {
	host, logger := newTestHost(t, Config{VirtualHost: "APP_ONE.INTERNAL"})
	browser := webview2.New()
	host.browser = browser
	var watchdog, navigation int

	err := host.startWebViewFirstNavigation(
		browser,
		func(...string) error { return nil },
		func(*webview2.Browser) error {
			host.windowDestroyed = true
			host.browser = nil
			browser.ShuttingDown()
			return nil
		},
		func() { watchdog++ },
		func() error { navigation++; return nil },
	)
	if err == nil {
		t.Fatal("optional lifecycle failure returned nil")
	}
	if watchdog != 0 || navigation != 0 {
		t.Fatalf("optional lifecycle failure watchdog=%d navigation=%d, want zero", watchdog, navigation)
	}
	for _, forbidden := range []string{"asset serving ready", "injected scripts registered", "navigate requested", "frontend ready", "host ready"} {
		if strings.Contains(logger.String(), forbidden) {
			t.Fatalf("optional lifecycle failure logged %q:\n%s", forbidden, logger.String())
		}
	}
}

func TestStartupLoggerTriggeredQuitStopsLaterReadinessWatchdogAndNavigate(t *testing.T) {
	logger := &debugReentrantLogger{captureLogger: &captureLogger{}}
	host := New(Config{Logger: logger})
	const hwnd = windowHandle(0x135)
	beginHeadlessLifecycleRun(t, host, hwnd)
	browser := webview2.New()
	host.browser = browser
	host.postNativeCommand = func(got windowHandle, message uint32, _ uintptr, _ uintptr) error {
		if got != hwnd || message != wmNativeQuit {
			t.Fatalf("quit post = (%#x, %#x), want (%#x, %#x)", got, message, hwnd, wmNativeQuit)
		}
		return nil
	}
	logger.onDebug = func() {
		host.Quit()
		// Quit's post holds host.mu.RLock while the fake observes it. Deliver the
		// queued command only after Quit returns, as the real message loop does.
		host.beginWindowDestroy(hwnd)
		host.windowDestroyTeardown()
	}

	var watchdog, navigation int
	err := host.startWebViewFirstNavigation(
		browser,
		func(...string) error { return nil },
		func(*webview2.Browser) error { return nil },
		func() { watchdog++ },
		func() error { navigation++; return nil },
	)
	if err == nil {
		t.Fatal("Logger-triggered Quit continued startup")
	}
	if watchdog != 0 || navigation != 0 {
		t.Fatalf("Logger-triggered Quit watchdog=%d navigation=%d, want zero", watchdog, navigation)
	}
	if host.browser != nil || !browser.IsShuttingDown() {
		t.Fatalf("Logger-triggered Quit browser committed=%v shuttingDown=%v, want false true", host.browser == browser, browser.IsShuttingDown())
	}
	logged := logger.String()
	if got := strings.Count(logged, "asset serving ready, source=embedded-fs"); got != 1 {
		t.Fatalf("Logger-triggered Quit asset-ready trigger count=%d, want one:\n%s", got, logged)
	}
	for _, forbidden := range []string{"injected scripts registered", "navigate requested", "frontend ready", "host ready"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("Logger-triggered Quit logged later success %q:\n%s", forbidden, logged)
		}
	}
}

func TestNavigateSuccessIsCommittedBeforeLoggerCanReenterAndTearDown(t *testing.T) {
	logger := &debugReentrantLogger{captureLogger: &captureLogger{}}
	host := New(Config{Logger: logger})
	const hwnd = windowHandle(0x136)
	beginHeadlessLifecycleRun(t, host, hwnd)
	browser := webview2.New()
	host.browser = browser
	host.postNativeCommand = func(got windowHandle, message uint32, _ uintptr, _ uintptr) error {
		if got != hwnd || message != wmNativeQuit {
			t.Fatalf("quit post = (%#x, %#x), want (%#x, %#x)", got, message, hwnd, wmNativeQuit)
		}
		return nil
	}

	var navigated, teardownInsideNavigateLog bool
	var reenter func()
	reenter = func() {
		if !strings.Contains(logger.String(), "mullion: navigate requested") {
			logger.onDebug = reenter
			return
		}
		teardownInsideNavigateLog = navigated
		host.Quit()
		host.beginWindowDestroy(hwnd)
		host.windowDestroyTeardown()
	}
	logger.onDebug = reenter

	var watchdog, navigation int
	err := host.startWebViewFirstNavigation(
		browser,
		func(...string) error { return nil },
		func(*webview2.Browser) error { return nil },
		func() { watchdog++ },
		func() error {
			navigation++
			navigated = true
			return nil
		},
	)
	if err == nil {
		t.Fatal("Logger teardown after Navigate reported startup success")
	}
	if !teardownInsideNavigateLog {
		t.Fatal("navigate diagnostic ran before the successful Navigate effect")
	}
	if watchdog != 1 || navigation != 1 {
		t.Fatalf("Logger teardown watchdog=%d navigation=%d, want exactly 1 and 1", watchdog, navigation)
	}
	if host.browser != nil || !browser.IsShuttingDown() {
		t.Fatalf("Logger teardown browser committed=%v shuttingDown=%v, want false true", host.browser == browser, browser.IsShuttingDown())
	}
	logged := logger.String()
	if got := strings.Count(logged, "mullion: navigate requested"); got != 1 {
		t.Fatalf("navigate diagnostic count=%d, want one after the effect:\n%s", got, logged)
	}
	for _, forbidden := range []string{"frontend ready", "host ready"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("Logger teardown logged later success %q:\n%s", forbidden, logged)
		}
	}
}

func TestScriptRegistrationFailureHasOneShowTerminalReport(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})
	browser := webview2.New()
	host.browser = browser
	failure := errors.New("completion failed")
	var requiredRegistration, optionalTabStrip, watchdog, navigate, postEnsureHWNDAndController int

	visible := host.showFromMessageWithEffects(func(string) error {
		return host.startWebViewFirstNavigation(
			browser,
			func(...string) error { requiredRegistration++; return failure },
			func(*webview2.Browser) error { optionalTabStrip++; return nil },
			func() { watchdog++ },
			func() error { navigate++; return nil },
		)
	}, func() bool {
		postEnsureHWNDAndController++
		return true
	})
	if visible {
		t.Fatal("Show reported visibility after required registration failure")
	}
	if requiredRegistration != 1 || optionalTabStrip != 0 || watchdog != 0 || navigate != 0 || postEnsureHWNDAndController != 0 {
		t.Fatalf("failed Show effects required=%d tab=%d watchdog=%d navigate=%d post_ensure_hwnd_controller=%d, want 1,0,0,0,0", requiredRegistration, optionalTabStrip, watchdog, navigate, postEnsureHWNDAndController)
	}
	logged := logger.String()
	if got := strings.Count(logged, failure.Error()); got != 1 {
		t.Fatalf("terminal failure reports = %d, want exactly 1:\n%s", got, logged)
	}
	if got := strings.Count(logged, "level=ERROR"); got != 1 {
		t.Fatalf("terminal error lines = %d, want exactly 1:\n%s", got, logged)
	}

	postEnsureHWNDAndController = 0
	if visible := host.showFromMessageWithEffects(func(string) error { return nil }, func() bool {
		postEnsureHWNDAndController++
		return true
	}); !visible || postEnsureHWNDAndController != 1 {
		t.Fatalf("successful Show apply visible=%v effects=%d, want true and one post-ensure effect", visible, postEnsureHWNDAndController)
	}
}

func TestShowProductionRouteCannotBypassPostEnsureEffectBoundary(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "control_windows.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			functions[function.Name.Name] = function
		}
	}
	show := functions["showFromMessage"]
	withEnsure := functions["showFromMessageWithEnsure"]
	withEffects := functions["showFromMessageWithEffects"]
	apply := functions["applyShowAfterEnsure"]
	if show == nil || withEnsure == nil || withEffects == nil || apply == nil {
		t.Fatal("production Show route or its post-ensure effect boundary is missing")
	}
	calls := func(function *ast.FuncDecl, name string) []*ast.CallExpr {
		var found []*ast.CallExpr
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok && webViewASTSelectorPath(call.Fun) == name {
				found = append(found, call)
			}
			return true
		})
		return found
	}
	if got := len(calls(show, "host.showFromMessageWithEnsure")); got != 1 {
		t.Fatalf("showFromMessage production delegation calls = %d, want 1", got)
	}
	delegations := calls(withEnsure, "host.showFromMessageWithEffects")
	if len(delegations) != 1 || len(delegations[0].Args) != 2 || webViewASTSelectorPath(delegations[0].Args[1]) != "host.applyShowAfterEnsure" {
		t.Fatal("showFromMessageWithEnsure must delegate exactly once with the production post-ensure apply effect")
	}
	applyCalls := calls(withEffects, "apply")
	if len(applyCalls) != 1 {
		t.Fatalf("post-ensure apply calls = %d, want 1", len(applyCalls))
	}
	var ensureGuard *ast.IfStmt
	ast.Inspect(withEffects.Body, func(node ast.Node) bool {
		guard, ok := node.(*ast.IfStmt)
		if ok && guard.Init != nil && len(calls(&ast.FuncDecl{Body: &ast.BlockStmt{List: []ast.Stmt{guard}}}, "ensure")) == 1 {
			ensureGuard = guard
		}
		return true
	})
	if ensureGuard == nil || applyCalls[0].Pos() <= ensureGuard.End() {
		t.Fatal("post-ensure HWND/controller apply must remain after the returning ensure failure guard")
	}
}

// TestCreateWebViewProductionWiresRequiredDocumentCreatedScriptBarrier is a
// mutation trip-wire. It follows createWebView into the runnable production
// startup seam, so moving an early Navigate or watchdog call into a helper
// cannot evade the behavioural counters above.
func TestCreateWebViewProductionWiresRequiredDocumentCreatedScriptBarrier(t *testing.T) {
	fset := token.NewFileSet()
	createFile, err := parser.ParseFile(fset, "webview_windows.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	barrierFile, err := parser.ParseFile(fset, "webview_script_registration_windows.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	var webViewFunctions []*ast.FuncDecl
	for _, file := range []*ast.File{createFile, barrierFile} {
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok {
				functions[function.Name.Name] = function
				if file == createFile {
					webViewFunctions = append(webViewFunctions, function)
				}
			}
		}
	}
	create, startup, barrier := functions["createWebView"], functions["startWebViewFirstNavigation"], functions["registerRequiredDocumentCreatedScripts"]
	if create == nil || startup == nil || barrier == nil {
		t.Fatal("production create route, startup seam, or required script barrier is missing")
	}
	calls := func(function *ast.FuncDecl, name string) []*ast.CallExpr {
		var found []*ast.CallExpr
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok && webViewASTSelectorPath(call.Fun) == name {
				found = append(found, call)
			}
			return true
		})
		return found
	}
	exactlyOne := func(function *ast.FuncDecl, name string) *ast.CallExpr {
		found := calls(function, name)
		if len(found) != 1 {
			t.Fatalf("%s calls in %s = %d, want 1", name, function.Name.Name, len(found))
		}
		return found[0]
	}

	seam := exactlyOne(create, "host.startWebViewFirstNavigation")
	if len(seam.Args) != 5 || webViewASTSelectorPath(seam.Args[0]) != "browser" || webViewASTSelectorPath(seam.Args[1]) != "browser.RegisterDocumentCreatedScripts" || webViewASTSelectorPath(seam.Args[3]) != "host.startRenderWatchdog" {
		t.Fatal("create route must pass production registration and watchdog effects to the startup seam")
	}
	navigation, ok := seam.Args[4].(*ast.FuncLit)
	if !ok {
		t.Fatal("create route must pass Navigate as the startup seam effect")
	}
	var navigates []*ast.CallExpr
	for _, function := range webViewFunctions {
		navigates = append(navigates, calls(function, "browser.Navigate")...)
	}
	if len(navigates) != 1 {
		t.Fatalf("webview production browser.Navigate calls = %d, want one startup-seam effect", len(navigates))
	}
	navigateInsideEffect := false
	ast.Inspect(navigation.Body, func(node ast.Node) bool {
		if node == navigates[0] {
			navigateInsideEffect = true
		}
		return true
	})
	if !navigateInsideEffect {
		t.Fatal("production Navigate bypasses the startup seam effect")
	}
	for _, function := range webViewFunctions {
		if got := len(calls(function, "host.startRenderWatchdog")); got != 0 {
			t.Fatalf("%s contains %d direct watchdog arm(s)", function.Name.Name, got)
		}
	}
	registration := exactlyOne(startup, "host.registerRequiredDocumentCreatedScripts")
	watchdog := exactlyOne(startup, "startWatchdog")
	var navigateStep *ast.CallExpr
	for _, step := range calls(startup, "host.committedBrowserStepOrTearDown") {
		if len(step.Args) == 1 && webViewASTSelectorPath(step.Args[0]) == "navigate" {
			if navigateStep != nil {
				t.Fatal("Navigate must have one committed teardown owner")
			}
			navigateStep = step
		}
	}
	if navigateStep == nil {
		t.Fatal("Navigate must be passed to committedBrowserStepOrTearDown")
	}
	if !(registration.Pos() < watchdog.Pos() && watchdog.Pos() < navigateStep.Pos()) {
		t.Fatalf("script barrier must precede watchdog and committed Navigate: barrier=%s watchdog=%s navigate=%s", fset.Position(registration.Pos()), fset.Position(watchdog.Pos()), fset.Position(navigateStep.Pos()))
	}
	var navigateLogs []token.Pos
	ast.Inspect(startup.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || webViewASTSelectorPath(call.Fun) != "host.log.Debug" || len(call.Args) != 1 {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if ok && strings.Contains(literal.Value, "navigate requested") {
			navigateLogs = append(navigateLogs, call.Pos())
		}
		return true
	})
	if len(navigateLogs) != 1 || navigateLogs[0] <= navigateStep.Pos() {
		t.Fatalf("navigate success log positions=%v, want exactly one after committed Navigate at %s", navigateLogs, fset.Position(navigateStep.Pos()))
	}
	var lifecycleBeforeLog, lifecycleAfterLog bool
	for _, check := range calls(startup, "host.requireCurrentBrowser") {
		if navigateStep.Pos() < check.Pos() && check.Pos() < navigateLogs[0] {
			lifecycleBeforeLog = true
		}
		if navigateLogs[0] < check.Pos() {
			lifecycleAfterLog = true
		}
	}
	if !lifecycleBeforeLog || !lifecycleAfterLog {
		t.Fatalf("Navigate log lifecycle guards before=%v after=%v, want both", lifecycleBeforeLog, lifecycleAfterLog)
	}
	var registrationGuard *ast.IfStmt
	ast.Inspect(startup.Body, func(node ast.Node) bool {
		guard, ok := node.(*ast.IfStmt)
		if ok && guard.Init != nil {
			ast.Inspect(guard.Init, func(child ast.Node) bool {
				if child == registration {
					registrationGuard = guard
				}
				return true
			})
		}
		return true
	})
	if registrationGuard == nil {
		t.Fatal("required script barrier must return from its own failure guard")
	}
	noPreBarrierSuccessLog := func(function *ast.FuncDecl) {
		for _, success := range []string{"asset serving ready, source=embedded-fs", "injected scripts registered", "navigate requested"} {
			var positions []token.Pos
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || webViewASTSelectorPath(call.Fun) != "host.log.Debug" || len(call.Args) != 1 {
					return true
				}
				literal, ok := call.Args[0].(*ast.BasicLit)
				if ok && strings.Contains(literal.Value, success) {
					positions = append(positions, call.Pos())
				}
				return true
			})
			if len(positions) != 0 {
				t.Fatalf("%s contains %d pre-barrier success log(s) %q", function.Name.Name, len(positions), success)
			}
		}
	}
	for _, function := range webViewFunctions {
		if function != startup {
			noPreBarrierSuccessLog(function)
		}
	}
	noPreBarrierSuccessLog(barrier)
	for _, success := range []string{"asset serving ready, source=embedded-fs", "injected scripts registered", "navigate requested"} {
		var positions []token.Pos
		ast.Inspect(startup.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || webViewASTSelectorPath(call.Fun) != "host.log.Debug" || len(call.Args) != 1 {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if ok && strings.Contains(literal.Value, success) {
				positions = append(positions, call.Pos())
			}
			return true
		})
		if len(positions) != 1 || positions[0] <= registrationGuard.End() {
			t.Fatalf("success log %q must be uniquely reachable after the barrier succeeds", success)
		}
	}

	for _, function := range []*ast.FuncDecl{startup, barrier} {
		for _, forbidden := range []string{"browser.Navigate", "browser.Embed", "host.startRenderWatchdog", "host.syncWebViewBounds"} {
			if got := len(calls(function, forbidden)); got != 0 {
				t.Fatalf("%s contains %d forbidden pre-barrier operation(s) %s", function.Name.Name, got, forbidden)
			}
		}
	}

	teardown := exactlyOne(barrier, "host.committedBrowserStepOrTearDown")
	register := exactlyOne(barrier, "register")
	if len(register.Args) != 4 {
		t.Fatalf("barrier registration arguments = %d, want four required scripts", len(register.Args))
	}
	want := []string{"host.js.bridge", "host.js.diagnostics", "host.js.drag", "host.js.resize"}
	for index, argument := range register.Args {
		if got := webViewASTSelectorPath(argument); got != want[index] {
			t.Fatalf("required script %d = %s, want %s", index, got, want[index])
		}
	}
	inside := false
	ast.Inspect(teardown.Args[0], func(node ast.Node) bool {
		if node == register {
			inside = true
		}
		return true
	})
	if !inside {
		t.Fatal("required script registration bypasses committedBrowserStepOrTearDown")
	}
}
