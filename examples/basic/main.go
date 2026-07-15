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
