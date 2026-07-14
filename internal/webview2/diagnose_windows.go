//go:build windows

package webview2

// Diagnostics answer a question the registry cannot. "Is a WebView2 runtime
// installed" is easy and nearly useless; the question that decides whether a
// window will open is "which runtime would *this* process load, and does it
// still export the entry point we call".
//
// The second half is the point. mullion drives the runtime's own client DLL
// directly (docs/decisions/0001) because the Evergreen runtime does not ship
// WebView2Loader.dll, and Microsoft documents that export as subject to change.
// A test pins its existence - but only on the machine that runs the test suite.
// The machine that matters is the user's, and until this existed there was no
// way to ask the question there.

// RuntimeReport describes the WebView2 runtime this process would load.
type RuntimeReport struct {
	// Folder is the runtime directory that was selected.
	Folder string

	// ClientDLL is the exact binary that would be loaded. When a browser fails
	// to start, this is the first question, and a version number is not an
	// answer to it.
	ClientDLL string

	// Version is the runtime's version, from the registry when it describes the
	// install, otherwise from the DLL's own version resource.
	Version string

	// Source names how the runtime was found: the environment pin, or which
	// registry view it came out of.
	Source string

	// Fixed is true for a fixed-version runtime pinned through
	// BrowserExecutableFolderEnv, which is a different report from an Evergreen
	// one and has to say so.
	Fixed bool

	// ExportName is the entry point mullion calls.
	ExportName string

	// ExportFound is true when the client DLL really exports it.
	ExportFound bool

	// ExportProblem says why it could not be resolved, when it could not.
	ExportProblem string
}

// DescribeRuntime runs the same discovery the host runs at startup, then loads
// the selected client DLL and resolves the export.
//
// Loading the DLL starts no browser process and creates no window: it maps the
// library and looks up one symbol. That is the same thing
// TestRuntimeExportsTheEntryPointWeCallDirectly does, deliberately - a
// diagnostic that exercises a different code path from the one it is diagnosing
// proves nothing about it.
//
// An error means no runtime could be selected at all. A report with ExportFound
// false means a runtime was found and cannot be driven.
func DescribeRuntime() (RuntimeReport, error) {
	found, err := findRuntime()
	if err != nil {
		return RuntimeReport{ExportName: createEnvironmentExport}, err
	}

	report := RuntimeReport{
		Folder:     found.Folder,
		ClientDLL:  found.ClientDLL,
		Version:    found.Version,
		Source:     string(found.Source),
		Fixed:      found.Fixed,
		ExportName: createEnvironmentExport,
	}

	if _, err := loadClient(found.ClientDLL); err != nil {
		// The runtime is on disk and cannot be used. That is a different failure
		// from "no runtime", and the report keeps both halves: the folder that
		// was chosen, and why it did not work.
		report.ExportProblem = err.Error()
		return report, nil
	}
	report.ExportFound = true
	return report, nil
}
