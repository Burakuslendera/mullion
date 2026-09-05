//go:build windows

package webview2

import "testing"

// DescribeRuntime performs real runtime discovery and client-DLL export lookup.
// It shares the explicit MULLION_REQUIRE_WEBVIEW2=1 gate with the other machine
// checks, so the default suite skips before any registry, filesystem, or DLL
// access; a required runtime/export that is absent fails the machine lane.
//
// What it locks is the contract the doctor prints: a report that names a
// runtime must name the binary it would load, say how it was found, and answer
// the export question without a silent success.
func TestDescribeRuntimeCannotBeSilentAboutTheExport(t *testing.T) {
	requireWebView2Machine(t)

	report, err := DescribeRuntime()

	// Even a failed discovery has to name the export it was going to look for.
	// A blank field in a diagnostic reads as "checked, fine".
	if report.ExportName != createEnvironmentExport {
		t.Fatalf("ExportName = %q, want %q even when discovery fails", report.ExportName, createEnvironmentExport)
	}
	if err != nil {
		t.Fatalf("%s=1 but no WebView2 runtime is available: %v", requireWebView2Env, err)
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
		t.Fatalf("%s=1 but the required export is missing: %s", requireWebView2Env, report.ExportProblem)
	}
	if report.ExportProblem != "" {
		t.Errorf("ExportFound is true but ExportProblem = %q; a report cannot say both", report.ExportProblem)
	}
}
