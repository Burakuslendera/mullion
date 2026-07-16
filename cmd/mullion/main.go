// Command mullion reports what a window needs from the machine it is about to
// run on, and whether this machine can give it.
//
//	go run github.com/Burakuslendera/mullion/cmd/mullion@latest doctor
//
// It takes no checkout and no PowerShell: the person filing a window bug report
// has the library and a Go toolchain by definition, and nothing else can be
// assumed. Environment is half of every frame or DPI report, and the half that
// used to be gathered by hand - which is how "1536x864" ends up in a report from
// a 1920x1080 monitor at 125%, and how an afternoon is lost to a scaling bug
// that was never there.
//
// The one line here that no registry lookup can produce: mullion drives the
// WebView2 runtime's own client DLL directly, and Microsoft documents that entry
// point as subject to change. doctor resolves it, on this machine, and says so.
//
// One capture helper lives beside it: backdrop covers the desktop with a flat
// colour while a window screenshot is taken (docs/decisions/0013).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Burakuslendera/mullion/host"
	"github.com/Burakuslendera/mullion/internal/backdrop"
	"github.com/Burakuslendera/mullion/internal/doctor"
)

func main() {
	command := ""
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	switch command {
	case "doctor":
		report := doctor.Probe(host.Version())
		fmt.Print(doctor.Format(report))
		if !report.Usable() {
			// The block above says what is wrong; the exit code says that
			// something is. Without it the command is readable only by a human,
			// and half of what asks these questions is a script.
			os.Exit(1)
		}

	case "backdrop":
		flags := flag.NewFlagSet("backdrop", flag.ExitOnError)
		colourHex := flags.String("colour", backdrop.DefaultHex, "backdrop colour, #rrggbb")
		class := flags.String("class", "MullionWindow", "window class to lift above the backdrop; empty to just cover the desktop")
		_ = flags.Parse(os.Args[2:]) // ExitOnError: a bad flag already exited 2
		colour, err := backdrop.ParseColour(*colourHex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mullion backdrop: %v\n", err)
			os.Exit(2)
		}
		fmt.Println("mullion: backdrop up - it closes with the window it lifts (close or" +
			" minimise that window); Esc on it, Alt+F4, or Ctrl+C here also close it.")
		if err := backdrop.Show(colour, *class); err != nil {
			fmt.Fprintf(os.Stderr, "mullion backdrop: %v\n", err)
			os.Exit(1)
		}

	case "version":
		fmt.Println(host.Version())

	case "help", "-h", "--help":
		usage(os.Stdout)

	default:
		if command != "" {
			fmt.Fprintf(os.Stderr, "mullion: unknown command %q\n\n", command)
		}
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(out io.Writer) {
	fmt.Fprint(out, `mullion - diagnostics and capture helpers for the mullion window host

Usage:
  mullion doctor    Print the environment a window bug report needs, and check
                    that the WebView2 runtime this machine would load still
                    exports the entry point mullion calls. Starts no browser and
                    opens no window.
  mullion backdrop  Cover every monitor with a flat colour while you screenshot
                    a window, so nothing of the desktop lands in the margin. A
                    visible mullion window is lifted in front of it as it opens
                    (-class overrides which window class that looks for; empty
                    skips it), and from then on the backdrop follows that
                    window: move and resize it freely, and the moment it is
                    closed, its process ends, or it is minimised, the backdrop
                    closes itself. It is not topmost - anything you raise stays
                    above it. Capture with any tool; Esc on the backdrop (or
                    Alt+F4, or Ctrl+C in this terminal) also dismisses it.
                    Windows only. -colour #rrggbb overrides the dark grey.
  mullion version   Print the version of mullion linked into this binary.
  mullion help      Print this message.

doctor exits 0 when mullion can start on this machine, and 1 when it cannot.

Run it without installing anything:
  go run github.com/Burakuslendera/mullion/cmd/mullion@latest doctor

Install it, which puts the binary in $(go env GOPATH)/bin - a directory that has
to be on your PATH for the bare name to work:
  go install github.com/Burakuslendera/mullion/cmd/mullion@latest

From a checkout, "go install ./cmd/mullion" stamps the commit into the binary
and "go run" does not, which is why the version line there says so.
`)
}
