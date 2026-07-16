// Package backdrop covers the desktop with a flat colour while a window
// screenshot is taken, so nothing of the desktop lands in the margin of a
// published image.
//
// It exists as a `mullion backdrop` command for the same reason doctor is one
// (docs/decisions/0008, 0013): the person capturing a window has the library
// and a Go toolchain, and `go run .../cmd/mullion@latest backdrop` needs no
// checkout and no PowerShell. scripts/screenshot.ps1 automates a whole capture
// from a checkout; this command is the piece of it that composes with any
// capture tool the user already likes.
//
// The colour parse is the command's entire input surface and is tested
// headlessly; the window half is a thin Win32 layer in backdrop_windows.go and
// opens nothing but the one popup window - no file, no socket, no log.
package backdrop

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// Colour is an opaque sRGB colour.
type Colour struct {
	R, G, B uint8
}

// DefaultHex is the backdrop's default: a dark neutral grey that reads as a
// studio ground rather than as content - the same value
// scripts/screenshot.ps1 uses for its own backdrop.
const DefaultHex = "#2b2d34"

// ParseColour accepts exactly #rrggbb or rrggbb, case-insensitive, and
// nothing else. The colour is the command's only input, so this parse is its
// whole input-validation surface, and it is strict on purpose: anything that
// is not six hex digits is rejected rather than guessed at.
func ParseColour(value string) (Colour, error) {
	digits := strings.TrimPrefix(value, "#")
	if len(digits) != 6 {
		return Colour{}, fmt.Errorf("colour must be #rrggbb, got %q", value)
	}
	decoded, err := hex.DecodeString(digits)
	if err != nil {
		return Colour{}, fmt.Errorf("colour must be #rrggbb, got %q", value)
	}
	return Colour{R: decoded[0], G: decoded[1], B: decoded[2]}, nil
}
