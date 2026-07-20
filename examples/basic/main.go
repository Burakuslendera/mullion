// Command basic is a minimal mullion host: a frameless window with a custom
// title bar, working caption buttons, working resize edges, and one
// application-defined bridge method.
//
// Run it from this directory:
//
//	go run .
package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/Burakuslendera/mullion/host"
)

//go:embed all:frontend
var embedded embed.FS

func main() {
	assets, err := fs.Sub(embedded, "frontend")
	if err != nil {
		log.Fatalf("embed frontend: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	host := host.New(host.Config{
		Assets: assets,

		// URL is an opt-in (docs/decisions/0012). Instead of serving the embedded
		// Assets over the in-process virtual host, it points the WebView at a
		// loopback origin you serve yourself - a dev server with hot reload, say, or
		// a runtime that already speaks HTTP. mullion still opens no socket: you run
		// the server, it only navigates there. When URL is set, Assets is optional
		// and is not served, but window.mullion (the bridge and the window controls)
		// is still injected, which is why URL is pinned to loopback.
		//
		// If that server is down - not running, or not up yet when the window
		// launches - the navigation fails. Rather than strand the user on Edge's
		// chromeless "can't reach this page" (which is not your frontend, so it has
		// no title bar and no caption buttons, and the frameless window looks
		// broken), mullion shows its OWN controllable fallback surface: a draggable
		// title bar, working minimise / maximise / close, a "Couldn't load <origin>"
		// message with the path and any token dropped, and a Retry that re-navigates
		// to the origin. It is a self-contained data: URL, so no socket is opened for
		// it either. See host/errorpage.go and issue #3.
		//
		// To watch the fallback appear, uncomment the line below and `go run .` with
		// nothing listening on that port - it shows at once. Then serve something on
		// the same origin in another terminal and click Retry to recover, e.g. the
		// demo frontend over the IPv6 loopback:
		//
		//	cd frontend && python -m http.server 39517 --bind ::1
		//
		// (any loopback origin works; [::1] is used here only because it is a literal
		// this repository's no-socket test permits inside an example - see leak_test.)
		//
		// URL: "http://[::1]:39517",

		Title:  "Mullion Basic",
		Width:  980,
		Height: 640,

		// These three must match the frontend's CSS. The title bar height and the
		// caption button cluster width define the native hit-test bands; if the
		// CSS says 36px and this says 32px, the top 4px of the visible title bar
		// would not drag.
		TitlebarHeight:       36,
		CaptionControlsWidth: 138, // 3 buttons x 46px
		ResizeBorder:         8,

		// DevTools keeps the developer surface on: F12 opens the browser DevTools,
		// and the default context menu and browser accelerator keys stay enabled.
		// Off by default - a frameless app window is not a browser - but uncomment
		// it while developing your frontend, or to poke the injected bridge from
		// the DevTools console (try window.mullion.invoke("Ping")). See
		// Config.DevTools in host/config.go.
		//
		// DevTools: true,

		BackgroundColour: host.Colour{R: 0x16, G: 0x1a, B: 0x22, A: 0xff},

		Logger: host.SlogLogger(logger),
		Bridge: bridge,
	})

	if err := host.Run(); err != nil {
		log.Fatalf("run: %v", err)
	}
}

// bridge handles the application's own methods. Window controls (drag, resize,
// minimise, maximise, close) never arrive here: mullion answers those itself, so
// this function only ever sees what the frontend explicitly invokes.
func bridge(raw string) string {
	var request struct {
		ID     string            `json:"id"`
		Method string            `json:"method"`
		Args   []json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		return ""
	}

	switch request.Method {
	case "Ping":
		return reply(request.ID, "pong from Go")
	case "Now":
		return reply(request.ID, time.Now().Format(time.RFC1123))
	default:
		return replyError(request.ID, "unknown method: "+request.Method)
	}
}

func reply(id string, result any) string {
	payload, err := json.Marshal(struct {
		ID     string `json:"id"`
		OK     bool   `json:"ok"`
		Result any    `json:"result"`
	}{ID: id, OK: true, Result: result})
	if err != nil {
		return replyError(id, "encode failed")
	}
	return string(payload)
}

func replyError(id string, reason string) string {
	payload, err := json.Marshal(struct {
		ID    string `json:"id"`
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}{ID: id, OK: false, Error: reason})
	if err != nil {
		return ""
	}
	return string(payload)
}
