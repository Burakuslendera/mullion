package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestPrintableVersionReducesReplacementPaths(t *testing.T) {
	version := "v0.1.0 (replaced by C:" + `\` + "Users" + `\` + "Alice" + `\dev\mullion` + ")"
	got := printableVersion(version)
	if strings.Contains(got, "Alice") {
		t.Fatalf("printable version disclosed the replacement path: %q", got)
	}
	for _, want := range []string{"v0.1.0", "mullion"} {
		if !strings.Contains(got, want) {
			t.Fatalf("printable version discarded %q: %q", want, got)
		}
	}
}

func TestMainVersionDispatchUsesPublicOutputBoundary(t *testing.T) {
	originalArgs, originalOutput, originalValue := os.Args, versionCommandOutput, versionCommandValue
	t.Cleanup(func() {
		os.Args = originalArgs
		versionCommandOutput = originalOutput
		versionCommandValue = originalValue
	})

	version := "v0.1.0 (replaced by C:" + `\` + "Users" + `\` + "Alice" + `\dev\mullion` + ")"
	var output bytes.Buffer
	os.Args = []string{"mullion", "version"}
	versionCommandOutput = &output
	versionCommandValue = func() string { return version }
	main()

	got := output.String()
	if strings.Contains(got, "Alice") {
		t.Fatalf("version command disclosed the replacement path: %q", got)
	}
	for _, want := range []string{"v0.1.0", "mullion"} {
		if !strings.Contains(got, want) {
			t.Fatalf("version command discarded %q: %q", want, got)
		}
	}
}
