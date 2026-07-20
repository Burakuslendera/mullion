//go:build !windows

package host

import "io"

// isTerminal is always false off Windows: the window host returns
// ErrUnsupportedPlatform there, so ColourLogger degrades to plain text - and
// never a VT refusal, so no degradation notice is logged. This stub keeps the
// package building on every OS (GOOS=linux go build ./...).
func isTerminal(io.Writer) (colour, vtRefused bool) { return false, false }
