//go:build windows

package doctor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// This test injects the result at the boundary before DescribeRuntime would
// inspect an export. It creates no window and loads no WebView2 DLL.
func TestDescribeWebView2MapsUnsupportedArchitectureWithoutExportState(t *testing.T) {
	calls := 0
	section := describeWebView2With(func() (webview2.RuntimeReport, error) {
		calls++
		return webview2.RuntimeReport{
			Folder:      `C:\must-not-be-reported`,
			ExportName:  "CreateWebViewEnvironmentWithOptionsInternal",
			ExportFound: true,
		}, fmt.Errorf("%w: GOARCH=arm64; WebView2 hosting is supported only on windows/amd64", webview2.ErrUnsupportedArchitecture)
	})

	if calls != 1 {
		t.Fatalf("DescribeRuntime seam calls = %d, want 1", calls)
	}
	if section.Found || section.ExportFound || section.Folder != "" {
		t.Fatalf("unsupported architecture mapped to runtime state: %+v", section)
	}
	for _, want := range []string{"GOARCH=arm64", "windows/amd64"} {
		if !strings.Contains(section.Problem, want) {
			t.Errorf("problem = %q, want %q", section.Problem, want)
		}
	}
	if section.ExportName == "" {
		t.Fatal("unsupported report lost the export name the diagnostic intended to inspect")
	}
	if (Report{WebView2: section}).Usable() {
		t.Fatal("unsupported architecture report must not be usable")
	}
}
