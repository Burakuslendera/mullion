//go:build windows

package webview2

// liveServerCount reports every registered and GC-rooted Go COM server, whether
// its references are held by this package, a fake caller, or the runtime. Tests
// use it to detect retained registrations; the count does not identify the
// current reference holder.
func liveServerCount() int {
	serversMu.Lock()
	defer serversMu.Unlock()
	return len(servers)
}
