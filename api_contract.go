package mullion

// hostAPI is the exported surface of Host. The assertion below is compiled on
// every platform, so the Windows implementation and the fallback stub cannot
// drift apart: adding a method to one without the other stops being a runtime
// surprise on someone else's machine and becomes a build failure here.
type hostAPI interface {
	Run() error
	Show() error
	Hide()
	Quit()
	Minimise()
	ToggleMaximise()
	StartDrag()
	StartResize(edge string)
	IsMaximised() bool
	SetTitle(title string)
	MarkFrontendShellReady()
	MarkFrontendReady()
	MarkFrontendPhase(phase string)
	MarkFrontendDiagnostic(kind string, detail string)
}

var _ hostAPI = (*Host)(nil)
