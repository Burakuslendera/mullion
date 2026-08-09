//go:build windows

package webview2

import (
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type loaderCallMode uint8

const (
	loaderCallFailsSynchronously loaderCallMode = iota
	loaderCallTimesOut
	loaderCallCompletesWithFailure
	loaderCallSucceeds
)

func packageCompletionHandler(this uintptr, iid windows.GUID) (*IUnknown, bool) {
	server := serverFor(this)
	if server == nil || server.iid != iid {
		return nil, false
	}
	handler, ok := server.self.(*completedHandler)
	if !ok || handler.this != this {
		return nil, false
	}
	return (*IUnknown)(unsafe.Pointer(handler)), true
}

type environmentNativeCall struct {
	mode loaderCallMode

	checkRunningInstance uintptr
	runtimeType          uintptr
	userDataFolder       string
	optionsThis          uintptr
	handlerThis          uintptr
	optionsRegistered    bool
	handlerRegistered    bool
	handlerSemanticQI    bool
	additionalArgs       string
	language             string
	targetVersion        string
	allowSSO             int32
	getterResults        [4]uintptr
	invokeResult         uintptr

	completionResult *IUnknown
	heldHandler      *IUnknown
}

func (c *environmentNativeCall) invoke(checkRunningInstance, runtimeType, userDataFolder, optionsThis, handlerThis uintptr) uintptr {
	c.checkRunningInstance = checkRunningInstance
	c.runtimeType = runtimeType
	c.optionsThis = optionsThis
	c.handlerThis = handlerThis

	optionsServer := serverFor(optionsThis)
	ownedOptions := optionsFor(optionsThis)
	c.optionsRegistered = optionsServer != nil &&
		optionsServer.iid == iidEnvironmentOptions &&
		ownedOptions != nil &&
		ownedOptions.this == optionsThis
	handler, handlerRegistered := packageCompletionHandler(handlerThis, iidEnvironmentCompletedHandler)
	c.handlerRegistered = handlerRegistered
	if c.optionsRegistered {
		if userDataFolder != 0 {
			c.userDataFolder = utf16At(userDataFolder)
		}
		c.additionalArgs, c.getterResults[0] = nativeOptionString(optionsThis, 3)
		c.language, c.getterResults[1] = nativeOptionString(optionsThis, 5)
		c.targetVersion, c.getterResults[2] = nativeOptionString(optionsThis, 7)
		c.getterResults[3] = callCOMSlot(
			unsafe.Pointer(&environmentOptionsVtable),
			9,
			optionsThis,
			uintptr(unsafe.Pointer(&c.allowSSO)),
		)
	}

	if c.mode == loaderCallTimesOut && c.handlerRegistered {
		c.heldHandler = handler
		c.heldHandler.AddRef() // the fake runtime retains the asynchronous callback
	}

	if c.handlerRegistered {
		queried, err := handler.QueryInterface(&iidEnvironmentCompletedHandler)
		if err == nil {
			c.handlerSemanticQI = true
			semantic := (*IUnknown)(queried)
			if c.mode == loaderCallCompletesWithFailure || c.mode == loaderCallSucceeds {
				completionHR := sOK
				if c.mode == loaderCallCompletesWithFailure {
					completionHR = eFail
				}
				c.invokeResult = callCOMSlot(
					unsafe.Pointer(semantic.Vtbl),
					3,
					uintptr(unsafe.Pointer(semantic)),
					completionHR,
					uintptr(unsafe.Pointer(c.completionResult)),
				)
			}
			semantic.Release()
		}
	}

	if c.mode == loaderCallFailsSynchronously {
		return eFail
	}
	if !c.handlerSemanticQI {
		return eFail
	}
	return sOK
}

func (c *environmentNativeCall) completeLate() {
	if c.heldHandler == nil {
		return
	}
	c.invokeResult = callCOMSlot(
		unsafe.Pointer(c.heldHandler.Vtbl),
		3,
		uintptr(unsafe.Pointer(c.heldHandler)),
		sOK,
		uintptr(unsafe.Pointer(c.completionResult)),
	)
	c.heldHandler.Release()
	c.heldHandler = nil
}

func nativeOptionString(optionsThis, slot uintptr) (string, uintptr) {
	var value uintptr
	hr := callCOMSlot(
		unsafe.Pointer(&environmentOptionsVtable),
		slot,
		optionsThis,
		uintptr(unsafe.Pointer(&value)),
	)
	if hr != sOK || value == 0 {
		return "", hr
	}
	text := utf16At(value)
	freeCoTaskMem(value)
	return text, hr
}

func TestCreateEnvironmentWithOptionsNativeCallBoundary(t *testing.T) {
	const (
		folder   = `C:\mullion-loader-boundary`
		args     = "--disable-features=ElasticOverscroll"
		language = "en-GB"
		target   = "123.4.5.6"
	)
	options := Options{
		UserDataFolder:                         folder,
		AdditionalBrowserArguments:             args,
		Language:                               language,
		TargetCompatibleBrowserVersion:         target,
		AllowSingleSignOnUsingOSPrimaryAccount: true,
		Timeout:                                time.Millisecond,
	}
	found := resolved{Fixed: true, Version: "124.0.0.0"}

	for _, test := range []struct {
		name string
		mode loaderCallMode
	}{
		{"synchronous failure", loaderCallFailsSynchronously},
		{"timeout and late result", loaderCallTimesOut},
		{"completed failure", loaderCallCompletesWithFailure},
		{"success", loaderCallSucceeds},
	} {
		t.Run(test.name, func(t *testing.T) {
			baseline := liveServerCount()
			result, resultState := newFakeUnknown(t)
			call := &environmentNativeCall{mode: test.mode, completionResult: result}
			t.Cleanup(func() {
				if call.heldHandler != nil {
					call.heldHandler.Release()
					call.heldHandler = nil
				}
			})
			proc := ComProc(windows.NewCallback(call.invoke))

			environment, err := createEnvironmentWithProc(options, found, proc)

			if call.checkRunningInstance != 1 {
				t.Errorf("checkRunningInstance = %d, want 1", call.checkRunningInstance)
			}
			if call.runtimeType != runtimeTypeFixed {
				t.Errorf("runtimeType = %d, want fixed runtime %d", call.runtimeType, runtimeTypeFixed)
			}
			if call.userDataFolder != folder {
				t.Errorf("user data folder = %q, want %q", call.userDataFolder, folder)
			}
			if !call.optionsRegistered {
				t.Error("native options argument is not the package-owned environment-options object")
			}
			if !call.handlerRegistered {
				t.Error("native handler argument is not the package-owned environment completion handler")
			}
			if !call.handlerSemanticQI {
				t.Error("environment completion handler refused its semantic IID")
			}
			if call.additionalArgs != args || call.language != language || call.targetVersion != target || call.allowSSO != 1 {
				t.Errorf("numerical option getters = (%q, %q, %q, %d), want (%q, %q, %q, 1)", call.additionalArgs, call.language, call.targetVersion, call.allowSSO, args, language, target)
			}
			for index, hr := range call.getterResults {
				if hr != sOK {
					t.Errorf("option getter %d HRESULT = %#x, want S_OK", index, hr)
				}
			}

			switch test.mode {
			case loaderCallFailsSynchronously:
				if err == nil || environment != nil {
					t.Fatalf("synchronous failure = %p, %v; want nil environment and error", environment, err)
				}
				assertLoaderRoots(t, baseline, call.optionsThis, call.handlerThis)
				assertFakeResultCounts(t, resultState, 0, 0)
			case loaderCallTimesOut:
				if err == nil || environment != nil {
					t.Fatalf("timeout = %p, %v; want nil environment and error", environment, err)
				}
				if serverFor(call.optionsThis) != nil {
					t.Error("environment options remained rooted after timeout")
				}
				if liveServerCount() != baseline+1 || serverFor(call.handlerThis) == nil {
					t.Fatalf("late handler roots = %d, want baseline+1 (%d) until the fake runtime releases it", liveServerCount(), baseline+1)
				}
				call.completeLate()
				if call.invokeResult != sOK {
					t.Errorf("late Invoke = %#x, want S_OK", call.invokeResult)
				}
				assertLoaderRoots(t, baseline, call.optionsThis, call.handlerThis)
				assertFakeResultCounts(t, resultState, 1, 1)
			case loaderCallCompletesWithFailure:
				if err == nil || environment != nil {
					t.Fatalf("completed failure = %p, %v; want nil environment and error", environment, err)
				}
				if call.invokeResult != sOK {
					t.Errorf("Invoke = %#x, want S_OK", call.invokeResult)
				}
				assertLoaderRoots(t, baseline, call.optionsThis, call.handlerThis)
				assertFakeResultCounts(t, resultState, 1, 1)
			case loaderCallSucceeds:
				if err != nil {
					t.Fatalf("success returned error: %v", err)
				}
				if environment == nil || environment.Unknown() != result {
					t.Fatalf("success environment = %p, raw %p; want result %p", environment, environment.Unknown(), result)
				}
				if call.invokeResult != sOK {
					t.Errorf("Invoke = %#x, want S_OK", call.invokeResult)
				}
				assertLoaderRoots(t, baseline, call.optionsThis, call.handlerThis)
				assertFakeResultCounts(t, resultState, 1, 0)
				environment.Release()
				assertFakeResultCounts(t, resultState, 1, 1)
			}
			runtime.KeepAlive(result)
			runtime.KeepAlive(call)
		})
	}
}

type controllerNativeCall struct {
	mode loaderCallMode

	expectedThis      uintptr
	actualThis        uintptr
	parent            uintptr
	handlerThis       uintptr
	handlerRegistered bool
	handlerSemanticQI bool
	invokeResult      uintptr

	completionResult *IUnknown
	heldHandler      *IUnknown
}

func (c *controllerNativeCall) invoke(this, parent, handlerThis uintptr) uintptr {
	c.actualThis = this
	c.parent = parent
	c.handlerThis = handlerThis
	handler, handlerRegistered := packageCompletionHandler(handlerThis, iidControllerCompletedHandler)
	c.handlerRegistered = handlerRegistered

	if c.mode == loaderCallTimesOut && c.handlerRegistered {
		c.heldHandler = handler
		c.heldHandler.AddRef()
	}

	if c.handlerRegistered {
		queried, err := handler.QueryInterface(&iidControllerCompletedHandler)
		if err == nil {
			c.handlerSemanticQI = true
			semantic := (*IUnknown)(queried)
			if c.mode == loaderCallCompletesWithFailure || c.mode == loaderCallSucceeds {
				completionHR := sOK
				if c.mode == loaderCallCompletesWithFailure {
					completionHR = eFail
				}
				c.invokeResult = callCOMSlot(
					unsafe.Pointer(semantic.Vtbl),
					3,
					uintptr(unsafe.Pointer(semantic)),
					completionHR,
					uintptr(unsafe.Pointer(c.completionResult)),
				)
			}
			semantic.Release()
		}
	}

	if c.mode == loaderCallFailsSynchronously {
		return eFail
	}
	if !c.handlerSemanticQI {
		return eFail
	}
	return sOK
}

func (c *controllerNativeCall) completeLate() {
	if c.heldHandler == nil {
		return
	}
	c.invokeResult = callCOMSlot(
		unsafe.Pointer(c.heldHandler.Vtbl),
		3,
		uintptr(unsafe.Pointer(c.heldHandler)),
		sOK,
		uintptr(unsafe.Pointer(c.completionResult)),
	)
	c.heldHandler.Release()
	c.heldHandler = nil
}

func TestEnvironmentCreateControllerNativeCallBoundary(t *testing.T) {
	const parent windows.Handle = 0x10203040

	for _, test := range []struct {
		name string
		mode loaderCallMode
	}{
		{"synchronous failure", loaderCallFailsSynchronously},
		{"timeout and late result", loaderCallTimesOut},
		{"completed failure", loaderCallCompletesWithFailure},
		{"success", loaderCallSucceeds},
	} {
		t.Run(test.name, func(t *testing.T) {
			baseline := liveServerCount()
			result, resultState := newFakeUnknown(t)
			call := &controllerNativeCall{mode: test.mode, completionResult: result}
			t.Cleanup(func() {
				if call.heldHandler != nil {
					call.heldHandler.Release()
					call.heldHandler = nil
				}
			})
			vtable := &ICoreWebView2EnvironmentVtbl{
				IUnknownVtbl:                 fakeComIUnknownVtbl,
				CreateCoreWebView2Controller: ComProc(windows.NewCallback(call.invoke)),
			}
			rawEnvironment := &IUnknown{Vtbl: (*IUnknownVtbl)(unsafe.Pointer(vtable))}
			call.expectedThis = uintptr(unsafe.Pointer(rawEnvironment))
			environment := &Environment{unknown: rawEnvironment}

			var controller *IUnknown
			var err error
			if test.mode == loaderCallTimesOut {
				controller, err = environment.createControllerWithTimeout(parent, time.Millisecond)
			} else {
				controller, err = environment.CreateController(parent)
			}

			if call.actualThis != call.expectedThis {
				t.Errorf("native environment this = %#x, want %#x", call.actualThis, call.expectedThis)
			}
			if call.parent != uintptr(parent) {
				t.Errorf("native parent HWND = %#x, want %#x", call.parent, uintptr(parent))
			}
			if !call.handlerRegistered {
				t.Error("native handler argument is not the package-owned controller completion handler")
			}
			if !call.handlerSemanticQI {
				t.Error("controller completion handler refused its semantic IID")
			}

			switch test.mode {
			case loaderCallFailsSynchronously:
				if err == nil || controller != nil {
					t.Fatalf("synchronous failure = %p, %v; want nil controller and error", controller, err)
				}
				assertLoaderRoots(t, baseline, call.handlerThis)
				assertFakeResultCounts(t, resultState, 0, 0)
			case loaderCallTimesOut:
				if err == nil || controller != nil {
					t.Fatalf("timeout = %p, %v; want nil controller and error", controller, err)
				}
				if liveServerCount() != baseline+1 || serverFor(call.handlerThis) == nil {
					t.Fatalf("late handler roots = %d, want baseline+1 (%d) until the fake runtime releases it", liveServerCount(), baseline+1)
				}
				call.completeLate()
				if call.invokeResult != sOK {
					t.Errorf("late Invoke = %#x, want S_OK", call.invokeResult)
				}
				assertLoaderRoots(t, baseline, call.handlerThis)
				assertFakeResultCounts(t, resultState, 1, 1)
			case loaderCallCompletesWithFailure:
				if err == nil || controller != nil {
					t.Fatalf("completed failure = %p, %v; want nil controller and error", controller, err)
				}
				if call.invokeResult != sOK {
					t.Errorf("Invoke = %#x, want S_OK", call.invokeResult)
				}
				assertLoaderRoots(t, baseline, call.handlerThis)
				assertFakeResultCounts(t, resultState, 1, 1)
			case loaderCallSucceeds:
				if err != nil {
					t.Fatalf("success returned error: %v", err)
				}
				if controller != result {
					t.Fatalf("success controller = %p, want result %p", controller, result)
				}
				if call.invokeResult != sOK {
					t.Errorf("Invoke = %#x, want S_OK", call.invokeResult)
				}
				assertLoaderRoots(t, baseline, call.handlerThis)
				assertFakeResultCounts(t, resultState, 1, 0)
				controller.Release()
				assertFakeResultCounts(t, resultState, 1, 1)
			}
			runtime.KeepAlive(result)
			runtime.KeepAlive(environment)
			runtime.KeepAlive(call)
		})
	}
}

func assertLoaderRoots(t *testing.T, baseline int, addresses ...uintptr) {
	t.Helper()
	for _, address := range addresses {
		if address != 0 && serverFor(address) != nil {
			t.Errorf("package-owned COM object %#x remained registered after its production release", address)
		}
	}
	if got := liveServerCount(); got != baseline {
		t.Errorf("live package COM objects = %d, want baseline %d", got, baseline)
	}
}

func assertFakeResultCounts(t *testing.T, state *fakeComState, addRefs, releases int) {
	t.Helper()
	if state.addRefs != addRefs || state.releases != releases {
		t.Errorf("result AddRef/Release = %d/%d, want %d/%d", state.addRefs, state.releases, addRefs, releases)
	}
}
