//go:build windows

package webview2

// liveServerCount reports how many Go-implemented COM objects the runtime still
// holds. It exists for the tests that prove a handler is released rather than
// leaked once creation completes, and it lives here because that is its only
// caller - it reads package-private state, so no production file needs it.
func liveServerCount() int {
	serversMu.Lock()
	defer serversMu.Unlock()
	return len(servers)
}
