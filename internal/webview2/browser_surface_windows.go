//go:build windows

package webview2

// The Browser's surface methods: the operations a host performs on an embedded
// control. Split from browser_windows.go, which keeps the type and Embed;
// event registration lives in browser_events_windows.go and teardown in
// browser_teardown_windows.go.

import (
	"errors"
)

// SetRasterizationScale updates the scale WebView2 rasterizes content at - the
// devicePixelRatio the frontend renders against.
//
// applyBoundsPolicy turns the runtime's own monitor-scale detection off, so the
// runtime never revises this scale on its own. After the host moves the window to
// a monitor with a different DPI it must set the new scale here, or the content
// keeps rendering at the scale of the monitor the controller was created on - too
// large on a lower-DPI monitor, too small on a higher one. The matching bounds are
// fed separately, in raw pixels, by the host's own DPI handling; the two do not
// compound because only the host drives either.
//
// The scale lives on ICoreWebView2Controller3. An older runtime without it is a
// warning to the caller, not a crash, exactly as in applyBoundsPolicy.
func (browser *Browser) SetRasterizationScale(scale float64) error {
	controller := browser.Controller()
	if controller == nil {
		return errors.New("webview2: controller unavailable")
	}
	controller3, err := controller.QueryController3()
	if err != nil {
		return err
	}
	defer controller3.Release()
	return controller3.PutRasterizationScale(scale)
}

// Navigate loads a URL.
func (browser *Browser) Navigate(url string) error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: navigate before embed")
	}
	return core.Navigate(url)
}

// Init registers a script to run in every document before any page script.
func (browser *Browser) Init(script string) error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: init before embed")
	}
	return core.AddScriptToExecuteOnDocumentCreated(script, nil)
}

// Eval runs a script in the current document.
func (browser *Browser) Eval(script string) error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: eval before embed")
	}
	return core.ExecuteScript(script, nil)
}

// Show makes the control visible.
//
// Showing the host window is not enough: the controller has its own visibility,
// and a controller left invisible renders nothing into a perfectly visible
// window.
func (browser *Browser) Show() error {
	controller := browser.Controller()
	if controller == nil {
		return errors.New("webview2: show before embed")
	}
	return controller.PutIsVisible(true)
}

// Hide makes the control invisible.
func (browser *Browser) Hide() error {
	controller := browser.Controller()
	if controller == nil {
		return errors.New("webview2: hide before embed")
	}
	return controller.PutIsVisible(false)
}

// NotifyParentWindowPositionChanged tells the control its host moved. Without
// it, anything the control positions in screen coordinates - the caret, an
// autofill popup - stays where the window used to be.
func (browser *Browser) NotifyParentWindowPositionChanged() error {
	controller := browser.Controller()
	if controller == nil {
		return errors.New("webview2: notify before embed")
	}
	return controller.NotifyParentWindowPositionChanged()
}

// SetBackgroundColour paints behind the page. It is what the user sees between
// the window appearing and the first frame being rendered, and during a resize.
func (browser *Browser) SetBackgroundColour(r, g, b, a uint8) error {
	controller := browser.Controller()
	if controller == nil {
		return errors.New("webview2: background before embed")
	}
	controller2, err := controller.QueryController2()
	if err != nil {
		return err
	}
	defer controller2.Release()
	return controller2.PutDefaultBackgroundColor(Color{A: a, R: r, G: g, B: b})
}

// Settings returns the base settings object. The pointer carries a reference
// the caller owns and must Release once it is done configuring.
func (browser *Browser) Settings() (*ICoreWebView2Settings, error) {
	core := browser.CoreWebView2()
	if core == nil {
		return nil, errors.New("webview2: settings before embed")
	}
	return core.GetSettings()
}

// AddWebResourceRequestedFilter subscribes the resource handler to a URI
// pattern. Without a filter the event never fires.
func (browser *Browser) AddWebResourceRequestedFilter(uri string, context WebResourceContext) error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: filter before embed")
	}
	return core.AddWebResourceRequestedFilter(uri, context)
}
