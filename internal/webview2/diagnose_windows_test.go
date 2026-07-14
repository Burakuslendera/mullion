//go:build windows

package webview2

import "testing"

// DescribeRuntime runs the real discovery and really loads the DLL, so like the
// other two machine-dependent tests in this package it skips when there is
// nothing installed to look at.
//
// What it locks is the contract the doctor prints, not the presence of the
// export: TestRuntimeExportsTheEntryPointWeCallDirectly already fails loudly if
// Microsoft removes it, and a second test failing for the same reason would
// teach nobody anything. This one locks that the *report* cannot be silent - a
// report that names a runtime must name the binary it would load, must say how
// it was found, and must answer the export question one way or the other.
func TestDescribeRuntimeCannotBeSilentAboutTheExport(t *testing.T) {
	report, err := DescribeRuntime()

	// Even a failed discovery has to name the export it was going to look for.
	// A blank field in a diagnostic reads as "checked, fine".
	if report.ExportName != createEnvironmentExport {
		t.Fatalf("ExportName = %q, want %q even when discovery fails", report.ExportName, createEnvironmentExport)
	}
	if err != nil {
		t.Skipf("no WebView2 runtime installed: %v", err)
	}

	if report.Folder == "" || report.ClientDLL == "" {
		t.Fatalf("folder=%q client=%q: a report that found a runtime must name the binary it would load",
			report.Folder, report.ClientDLL)
	}
	if report.Source == "" {
		t.Error("Source is empty: without it a pinned runtime reads as an installed one, and the reader reproduces against the wrong browser")
	}

	if !report.ExportFound {
		if report.ExportProblem == "" {
			t.Fatal("the export was not resolved and the report says nothing about why: that is the silent failure this package exists to prevent")
		}
		t.Logf("this runtime does not export %s: %s", createEnvironmentExport, report.ExportProblem)
		return
	}
	if report.ExportProblem != "" {
		t.Errorf("ExportFound is true but ExportProblem = %q; a report cannot say both", report.ExportProblem)
	}
}
