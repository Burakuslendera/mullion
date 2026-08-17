//go:build !windows

package host

// Host on a non-Windows platform is a stub that exists so a cross-platform
// program can import mullion, compile, and fail with a clear error at run time
// instead of failing to build.
//
// api_contract.go enforces the required methods and signatures enumerated there
// on every build; it does not assert complete method-set parity.
type Host struct {
	config    Config
	source    sourcePlan
	sourceErr error
}

// New prepares a host. On this platform the host cannot open a window; Run
// reports ErrUnsupportedPlatform.
func New(config Config) *Host {
	normalised := config.normalise()
	source, sourceErr := buildSourcePlan(normalised)
	return &Host{config: normalised, source: source, sourceErr: sourceErr}
}

// Run reports ErrUnsupportedPlatform. Check with errors.Is.
// ErrUnsupportedArchitecture is reserved for Windows processes whose ABI is
// unsupported; a non-Windows build remains a platform error.
func (host *Host) Run() error { return ErrUnsupportedPlatform }

// Show reports ErrUnsupportedPlatform.
func (host *Host) Show() error { return ErrUnsupportedPlatform }

// Hide is a no-op on non-Windows platforms.
func (host *Host) Hide() {}

// Quit is a no-op on non-Windows platforms.
func (host *Host) Quit() {}

// Minimise is a no-op on non-Windows platforms.
func (host *Host) Minimise() {}

// ToggleMaximise is a no-op on non-Windows platforms.
func (host *Host) ToggleMaximise() {}

// StartDrag is a no-op on non-Windows platforms.
func (host *Host) StartDrag() {}

// StartResize is a no-op on non-Windows platforms.
func (host *Host) StartResize(edge string) {}

// IsMaximised reports false on non-Windows platforms.
func (host *Host) IsMaximised() bool { return false }

// SetTitle is a no-op on non-Windows platforms.
func (host *Host) SetTitle(title string) {}

// MarkFrontendShellReady is a no-op on non-Windows platforms.
func (host *Host) MarkFrontendShellReady() {}

// MarkFrontendReady is a no-op on non-Windows platforms.
func (host *Host) MarkFrontendReady() {}

// MarkFrontendPhase is a no-op on non-Windows platforms.
func (host *Host) MarkFrontendPhase(phase string) {}

// MarkFrontendDiagnostic is a no-op on non-Windows platforms.
func (host *Host) MarkFrontendDiagnostic(kind, detail string) {}
