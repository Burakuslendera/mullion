package host

import (
	"net/url"
	"strings"
	"testing"
)

// These tests lock the fallback error surface (issue #3): a failed navigation must
// show mullion's own controllable page, not Edge's chromeless network-error page.
// They cover the PURE builder only. That the page actually renders with working
// caption buttons, drag and Retry is live behaviour and is NOT covered here - it
// needs a real window and the WebView2 runtime, and is called out in the report.
//
// The failed URLs below use the IPv6 loopback [::1]. TestNoNetworkListener
// (leak_test.go) forbids the two more common loopback literals outside loopback.go,
// and the builder does not validate the host anyway.

// decodeErrorPageHTML strips the data:text/html;charset=utf-8, prefix and
// percent-decodes the payload back to the HTML the WebView would parse. A decode
// failure means the builder did not produce a valid data: URL. Requiring the
// exact prefix here locks the explicit charset (issue #13): drop it from
// errorPageURL and every test through this helper fails on the prefix.
//
// It also asserts the payload ends exactly at the document: the template lives in
// errorpage.html, whose trailing newline(s) the builder must trim, and this is the
// one place every test passes through, so a regression cannot slip past it.
func decodeErrorPageHTML(t *testing.T, page string) string {
	t.Helper()
	const prefix = "data:text/html;charset=utf-8,"
	if !strings.HasPrefix(page, prefix) {
		t.Fatalf("error page is not a data:text/html;charset=utf-8 URL: %.40q", page)
	}
	if parsed, err := url.Parse(page); err != nil || parsed.Scheme != "data" {
		t.Fatalf("error page does not parse as a data URL: scheme=%q err=%v", parsed.Scheme, err)
	}
	decoded, err := url.PathUnescape(strings.TrimPrefix(page, prefix))
	if err != nil {
		t.Fatalf("error page payload is not valid percent-encoding: %v", err)
	}
	if strings.HasSuffix(decoded, "\n") {
		t.Errorf("error page payload carries a trailing newline; the builder must trim the template file's")
	}
	return decoded
}

func TestErrorPageURLShowsControllableSurfaceAndRedactsPath(t *testing.T) {
	const failed = "http://[::1]:8080/private?token=secret"
	config := Config{JSNamespace: "mullion", TitlebarHeight: 36}.normalise()

	doc := decodeErrorPageHTML(t, errorPageURL(config, failed))

	// Redaction: only the origin is shown, never the path, query or token.
	if !strings.Contains(doc, "http://[::1]:8080") {
		t.Errorf("error page does not show the failed origin")
	}
	for _, leak := range []string{"/private", "token", "secret"} {
		if strings.Contains(doc, leak) {
			t.Errorf("error page leaked %q from the failed URL - only the origin may appear", leak)
		}
	}

	// Caption buttons wired to the namespaced window controls. The injected bridge
	// shim installs window.<ns> on this document too, so these calls resolve.
	for _, call := range []string{
		"window.mullion.window.minimise()",
		"window.mullion.window.toggleMaximise()",
		"window.mullion.window.close()",
	} {
		if !strings.Contains(doc, call) {
			t.Errorf("error page is missing the caption control %q", call)
		}
	}

	// Drag title bar: the app-region drag surface, the fallback selector for old
	// runtimes, and a height that tracks Config.TitlebarHeight.
	if !strings.Contains(doc, "app-region: drag") {
		t.Errorf("error page has no app-region drag title bar")
	}
	if !strings.Contains(doc, "data-mullion-drag") {
		t.Errorf("error page is missing the fallback [data-<ns>-drag] attribute")
	}
	if !strings.Contains(doc, "height:36px") {
		t.Errorf("error page title bar does not use Config.TitlebarHeight (36px)")
	}

	// Retry re-navigates to the (redacted) origin.
	if !strings.Contains(doc, `href="http://[::1]:8080"`) {
		t.Errorf("error page Retry does not target the failed origin")
	}
}

func TestErrorPageURLUsesConfiguredNamespace(t *testing.T) {
	// A host configured for "app" must leave no addressable "mullion" behind, the
	// same contract js_test.go locks for the injected scripts.
	config := Config{JSNamespace: "app"}.normalise()
	doc := decodeErrorPageHTML(t, errorPageURL(config, "http://[::1]:9000"))

	if !strings.Contains(doc, "window.app.window.close()") {
		t.Errorf("caption controls are not wired to the configured namespace")
	}
	if !strings.Contains(doc, "data-app-drag") {
		t.Errorf("fallback drag attribute does not use the configured namespace")
	}
	for _, leak := range []string{"window.mullion", "data-mullion-"} {
		if strings.Contains(doc, leak) {
			t.Errorf("error page leaked the default namespace %q under a custom one", leak)
		}
	}
}

func TestErrorPageURLUsesConfiguredBackground(t *testing.T) {
	config := Config{BackgroundColour: Colour{R: 30, G: 30, B: 30, A: 255}}.normalise()
	doc := decodeErrorPageHTML(t, errorPageURL(config, "http://[::1]:9000"))

	if !strings.Contains(doc, "rgba(30,30,30,1)") {
		t.Errorf("error page does not paint the configured BackgroundColour")
	}
	// A dark background must get a light foreground, or the message is unreadable.
	if !strings.Contains(doc, "#f2f2f2") {
		t.Errorf("error page did not pick a legible foreground for a dark background")
	}
}
