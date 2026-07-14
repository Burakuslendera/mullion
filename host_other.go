//go:build !windows

package mullion

// Host on a non-Windows platform is a stub that exists so a cross-platform
// program can import mullion, compile, and fail with a clear error at run time
// instead of failing to build.
//
// Every method is present and every signature matches the Windows build - see
// api_contract.go, which enforces that at compile time.
type Host struct {
	config Config
}

// New prepares a host. On this platform the host cannot open a window; Run
// reports ErrUnsupportedPlatform.
func New(config Config) *Host {
	return &Host{config: config.normalise()}
}

// Run reports ErrUnsupportedPlatform. Check with errors.Is.
func (host *Host) Run() error { return ErrUnsupportedPlatform }

// Show reports ErrUnsupportedPlatform.
func (host *Host) Show() error { return ErrUnsupportedPlatform }

func (host *Host) Hide()                                      {}
func (host *Host) Quit()                                      {}
func (host *Host) Minimise()                                  {}
func (host *Host) ToggleMaximise()                            {}
func (host *Host) StartDrag()                                 {}
func (host *Host) StartResize(edge string)                    {}
func (host *Host) IsMaximised() bool                          { return false }
func (host *Host) SetTitle(title string)                      {}
func (host *Host) MarkFrontendShellReady()                    {}
func (host *Host) MarkFrontendReady()                         {}
func (host *Host) MarkFrontendPhase(phase string)             {}
func (host *Host) MarkFrontendDiagnostic(kind, detail string) {}
