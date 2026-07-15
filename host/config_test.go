package host

import (
	"testing"
	"time"
)

func TestConfigNormaliseFillsDefaults(t *testing.T) {
	config := Config{}.normalise()

	if config.Title != defaultTitle || config.ClassName != defaultClassName {
		t.Fatalf("title/class defaults = %q/%q", config.Title, config.ClassName)
	}
	if config.VirtualHost != defaultVirtualHost || config.JSNamespace != defaultJSNamespace {
		t.Fatalf("host/namespace defaults = %q/%q", config.VirtualHost, config.JSNamespace)
	}
	if config.Width != defaultWidth || config.Height != defaultHeight {
		t.Fatalf("size defaults = %dx%d", config.Width, config.Height)
	}
	if config.TitlebarHeight != defaultTitlebarHeight || config.CaptionControlsWidth != defaultCaptionControlsWidth || config.ResizeBorder != defaultResizeBorder {
		t.Fatalf("frame defaults = %d/%d/%d", config.TitlebarHeight, config.CaptionControlsWidth, config.ResizeBorder)
	}
	if config.ShowTimeout != defaultShowTimeout || config.RenderTimeout != defaultRenderTimeout {
		t.Fatalf("timeout defaults = %s/%s", config.ShowTimeout, config.RenderTimeout)
	}
	if config.DragSelector != "[data-mullion-drag]" {
		t.Fatalf("drag selector default = %q", config.DragSelector)
	}
	if config.Logger == nil {
		t.Fatal("Logger default is nil; every log call would panic")
	}
	if config.BackgroundColour.A != 255 {
		t.Fatalf("background default is not opaque: %+v", config.BackgroundColour)
	}
}

func TestConfigNormaliseKeepsExplicitValues(t *testing.T) {
	config := Config{
		Title:                "Acme",
		ClassName:            "AcmeWindow",
		VirtualHost:          "acme.internal",
		JSNamespace:          "acme",
		Width:                640,
		Height:               480,
		TitlebarHeight:       48,
		CaptionControlsWidth: 150,
		ResizeBorder:         6,
		ShowTimeout:          time.Second,
		RenderTimeout:        -1,
	}.normalise()

	if config.Title != "Acme" || config.ClassName != "AcmeWindow" || config.VirtualHost != "acme.internal" {
		t.Fatalf("explicit identity overwritten: %+v", config)
	}
	if config.JSNamespace != "acme" || config.DragSelector != "[data-acme-drag]" {
		t.Fatalf("drag selector did not follow the namespace: %q", config.DragSelector)
	}
	if config.TitlebarHeight != 48 || config.CaptionControlsWidth != 150 || config.ResizeBorder != 6 {
		t.Fatalf("explicit frame geometry overwritten: %+v", config)
	}
	if config.RenderTimeout != -1 {
		t.Fatal("a negative RenderTimeout (watchdog disabled) was overwritten by the default")
	}
}

// TestConfigHitTestMetricsFollowCSSUnlessOverridden locks the escape hatch: the
// native hit-test bands track the CSS geometry by default, and only diverge when
// the caller explicitly asks them to.
func TestConfigHitTestMetricsFollowCSSUnlessOverridden(t *testing.T) {
	metrics := Config{TitlebarHeight: 48, CaptionControlsWidth: 150}.normalise().hitTestMetrics()
	if metrics.TitlebarHeight != 48 || metrics.ControlsWidth != 150 {
		t.Fatalf("hit-test metrics did not follow the CSS geometry: %+v", metrics)
	}

	overridden := Config{
		TitlebarHeight:              48,
		CaptionControlsWidth:        150,
		HitTestTitlebarHeight:       32,
		HitTestCaptionControlsWidth: 138,
	}.normalise().hitTestMetrics()
	if overridden.TitlebarHeight != 32 || overridden.ControlsWidth != 138 {
		t.Fatalf("hit-test override ignored: %+v", overridden)
	}
}

// TestConfigRejectsInvalidJSNamespace guards a silent failure: the namespace is
// used both as a DOM attribute segment (data-<ns>-resize-edge) and as the
// camelCase dataset key that reads it back. A dash or an upper-case letter
// breaks that mapping without any error, and the resize edges simply stop
// responding.
func TestConfigRejectsInvalidJSNamespace(t *testing.T) {
	for _, namespace := range []string{"", "my-app", "My", "1x", "a b", "app.local"} {
		got := Config{JSNamespace: namespace}.normalise().JSNamespace
		if got != defaultJSNamespace {
			t.Fatalf("invalid namespace %q was accepted as %q", namespace, got)
		}
	}
	for _, namespace := range []string{"mullion", "acme", "app2"} {
		if got := (Config{JSNamespace: namespace}).normalise().JSNamespace; got != namespace {
			t.Fatalf("valid namespace %q was rewritten to %q", namespace, got)
		}
	}
}

// TestConfigOriginIsSingleSource locks the fix for a latent bug in the code this
// package grew out of: the navigation target and the asset allow-list were two
// separate literals, so changing one silently 403'd every asset.
func TestConfigOriginIsSingleSource(t *testing.T) {
	config := Config{VirtualHost: "acme.internal"}.normalise()
	if config.origin() != "https://acme.internal" {
		t.Fatalf("origin() = %q", config.origin())
	}
	if config.startURL() != "https://acme.internal/index.html" {
		t.Fatalf("startURL() = %q", config.startURL())
	}
}

func TestSlogLoggerNilIsSafe(t *testing.T) {
	logger := SlogLogger(nil)
	logger.Debug("no panic")
	logger.Error("no panic")
}
