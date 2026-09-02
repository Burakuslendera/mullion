package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type leakScanResult struct {
	output   string
	exitCode int
}

func TestLeakScanDetectsWindowsPathFamilies(t *testing.T) {
	drive := func(separator, suffix string) string {
		return "C:" + separator + "Users" + separator + "alice" + separator + suffix
	}
	unc := func(separator, host, share string) string {
		return separator + separator + host + separator + share + separator + "secret"
	}
	files := map[string][]byte{
		"backslash.txt":        []byte(drive(`\`, "secret")),
		"forward.txt":          []byte(drive(`/`, "secret")),
		"mixed.txt":            []byte("C:" + `\` + "Users/alice" + `\` + "secret"),
		"escaped.txt":          []byte(drive(`\\`, "secret")),
		"extended.txt":         []byte(`\\?\` + drive(`\`, "secret")),
		"unc.txt":              []byte(unc(`\`, "NAS01", "share")),
		"forward-unc.txt":      []byte(unc(`/`, "NAS01", "share")),
		"escaped-unc.txt":      []byte(unc(`\\`, "NAS01", "share")),
		"extended-unc.txt":     []byte(`\\?\UNC\` + "NAS01" + `\share\secret`),
		"punctuated-user.txt":  []byte("C:" + `\` + "Users" + `\` + "@alice" + `\secret`),
		"punctuated-share.txt": []byte(unc(`\`, "NAS01", "@share")),
	}
	root := newLeakScanRepository(t, files)
	result := runLeakScan(t, root)
	assertLeakScanFailed(t, result)
	for name := range files {
		if !strings.Contains(result.output, name) {
			t.Errorf("leak-scan did not report %s:\n%s", name, result.output)
		}
	}
}

func TestLeakScanKeepsPathControlsClean(t *testing.T) {
	placeholder := strings.Repeat(`\`, 2) + "<host>" + `\share\path`
	root := newLeakScanRepository(t, map[string][]byte{
		"controls.txt": []byte(strings.Join([]string{
			`C:relative\path`,
			"v1.2.3",
			"https://example.invalid/path",
			"https:" + strings.Repeat("/", 4) + "example.invalid/path",
			"https:" + strings.Repeat(`\`, 4) + "example.invalid" + `\path`,
			`%USERPROFILE%\wv2`,
			"C:" + `\` + "Users" + `\` + "<user>" + `\wv2`,
			placeholder,
		}, "\n")),
	})
	result := runLeakScan(t, root)
	if result.exitCode != 0 || !strings.Contains(result.output, "clean within configured scope") {
		t.Fatalf("clean controls did not receive a bounded clean verdict (exit %d):\n%s", result.exitCode, result.output)
	}
	for _, scope := range []string{"tracked text files scanned:", "commits scanned:", "binary files excluded:", "inspection errors: 0"} {
		if !strings.Contains(result.output, scope) {
			t.Errorf("clean verdict omitted %q:\n%s", scope, result.output)
		}
	}
}

func TestLeakScanAcceptsEquivalentWindowsRootSpellings(t *testing.T) {
	root := newLeakScanRepository(t, map[string][]byte{"clean.txt": []byte("clean\n")})
	top := strings.TrimSpace(gitOutput(t, root, "rev-parse", "--show-toplevel"))
	if strings.EqualFold(filepath.Clean(root), filepath.Clean(top)) {
		t.Skip("filesystem did not expose distinct equivalent path spellings")
	}
	result := runLeakScan(t, root)
	if result.exitCode != 0 || !strings.Contains(result.output, "clean within configured scope") {
		t.Fatalf("equivalent filesystem roots did not receive a clean verdict (exit %d):\n%s", result.exitCode, result.output)
	}
}

func TestLeakScanRejectsDifferentGitTopLevel(t *testing.T) {
	top := t.TempDir()
	root := filepath.Join(top, "nested")
	copyLeakScanScript(t, root)
	writeLeakScanFiles(t, root, map[string][]byte{"clean.txt": []byte("clean\n")})
	gitInit(t, top)
	gitRun(t, top, "add", "-A")
	gitRun(t, top, "commit", "-q", "-m", "clean fixture")

	result := runLeakScan(t, root)
	assertLeakScanFailed(t, result)
	if !strings.Contains(result.output, "does not match scanner root") {
		t.Fatalf("different Git top level did not fail at the root-identity boundary:\n%s", result.output)
	}
}

func TestLeakScanHasNoBasenameOrFilenameEscape(t *testing.T) {
	marker := "C:" + `\` + "Users" + `\` + "example" + `\secret`
	name := string(rune(0x00e9)) + "name.go"
	files := map[string][]byte{
		"nested/leak-scan.ps1": []byte(marker),
		"nested/leak_test.go":  []byte("package probe\n// " + marker + "\n"),
		"leak[x].go":           []byte("package probe\n// " + marker + "\n"),
		name:                   []byte("package probe\n// " + marker + "\n"),
	}
	root := newLeakScanRepository(t, files)
	result := runLeakScan(t, root)
	assertLeakScanFailed(t, result)
	for file := range files {
		if !strings.Contains(result.output, file) {
			t.Errorf("leak-scan did not report %q:\n%s", file, result.output)
		}
	}
}

func TestLeakScanFailsWhenDeclaredScopeCannotBeInspected(t *testing.T) {
	t.Run("missing tracked file", func(t *testing.T) {
		root := newLeakScanRepository(t, map[string][]byte{"missing.txt": []byte("clean\n")})
		if err := os.Remove(filepath.Join(root, "missing.txt")); err != nil {
			t.Fatalf("remove tracked file: %v", err)
		}
		assertLeakScanFailed(t, runLeakScan(t, root))
	})

	t.Run("invalid BOM-less UTF-8", func(t *testing.T) {
		root := newLeakScanRepository(t, map[string][]byte{"invalid.txt": {0xc3, 0x28}})
		assertLeakScanFailed(t, runLeakScan(t, root))
	})

	t.Run("invalid UTF-16LE", func(t *testing.T) {
		root := newLeakScanRepository(t, map[string][]byte{"invalid.txt": {0xff, 0xfe, 0x00}})
		assertLeakScanFailed(t, runLeakScan(t, root))
	})

	t.Run("invalid HEAD", func(t *testing.T) {
		root := newLeakScanRepository(t, map[string][]byte{"clean.txt": []byte("clean\n")})
		if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/missing\n"), 0o644); err != nil {
			t.Fatalf("corrupt HEAD: %v", err)
		}
		assertLeakScanFailed(t, runLeakScan(t, root))
	})

	t.Run("shallow history", func(t *testing.T) {
		source := newLeakScanRepository(t, map[string][]byte{"one.txt": []byte("one\n")})
		writeLeakScanFiles(t, source, map[string][]byte{"two.txt": []byte("two\n")})
		gitRun(t, source, "add", "-A")
		gitRun(t, source, "commit", "-q", "-m", "second")
		clone := filepath.Join(t.TempDir(), "shallow")
		url := "file:///" + strings.TrimPrefix(filepath.ToSlash(source), "/")
		gitRun(t, "", "clone", "-q", "--depth", "1", url, clone)
		assertLeakScanFailed(t, runLeakScan(t, clone))
	})

	t.Run("zero selected text files", func(t *testing.T) {
		root := t.TempDir()
		copyLeakScanScript(t, root)
		gitInit(t, root)
		if err := os.WriteFile(filepath.Join(root, "only.png"), []byte{0x89, 'P', 'N', 'G'}, 0o644); err != nil {
			t.Fatalf("write PNG: %v", err)
		}
		gitRun(t, root, "add", "only.png")
		gitRun(t, root, "commit", "-q", "-m", "binary only")
		assertLeakScanFailed(t, runLeakScan(t, root))
	})
}

func TestLeakScanRejectsUTF32BOMs(t *testing.T) {
	encode := func(text string, bigEndian bool) []byte {
		var encoded []byte
		if bigEndian {
			encoded = append(encoded, 0x00, 0x00, 0xfe, 0xff)
		} else {
			encoded = append(encoded, 0xff, 0xfe, 0x00, 0x00)
		}
		for _, character := range []byte(text) {
			if bigEndian {
				encoded = append(encoded, 0x00, 0x00, 0x00, character)
			} else {
				encoded = append(encoded, character, 0x00, 0x00, 0x00)
			}
		}
		return encoded
	}
	for _, testCase := range []struct {
		name      string
		bigEndian bool
	}{
		{"UTF-32LE BOM is fatal", false},
		{"UTF-32BE BOM is fatal", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			body := encode("C:\\Users\\private-user\\secret\n", testCase.bigEndian)
			root := newLeakScanRepository(t, map[string][]byte{"utf32.txt": body})
			result := runLeakScan(t, root)
			assertLeakScanFailed(t, result)
			// PowerShell can wrap the final word of an uncaught exception to the
			// Unix host width. The stable classifier plus the nonzero verdict
			// above proves this was the intended strict-decoding failure.
			if !strings.Contains(result.output, "unsupported UTF-32 byte order") {
				t.Fatalf("UTF-32 input was not rejected by strict decoding:\n%s", result.output)
			}
		})
	}
}

func TestLeakScanScansCommitMessages(t *testing.T) {
	marker := "C:" + `\` + "Users" + `\` + "example" + `\secret`
	root := newLeakScanRepositoryWithMessage(t, map[string][]byte{"clean.txt": []byte("clean\n")}, "leak in message: "+marker)
	writeLeakScanFiles(t, root, map[string][]byte{"tip.txt": []byte("clean tip\n")})
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "clean tip")
	result := runLeakScan(t, root)
	assertLeakScanFailed(t, result)
	if !strings.Contains(result.output, "commit") {
		t.Fatalf("commit-message finding was not identified:\n%s", result.output)
	}
}

func TestLeakScanForcesUTF8CommitOutput(t *testing.T) {
	marker := "C:/Users/" + "private-user/secret"
	root := newLeakScanRepositoryWithMessage(t, map[string][]byte{
		"clean.txt": []byte("clean\n"),
	}, "leak in message: "+marker)
	gitRun(t, root, "config", "i18n.commitEncoding", "UTF-16LE")
	gitRun(t, root, "config", "i18n.logOutputEncoding", "UTF-16LE")

	result := runLeakScan(t, root)
	assertLeakScanFailed(t, result)
	if !strings.Contains(result.output, "commit") {
		t.Fatalf("configured Git output encoding hid the reachable message:\n%s", result.output)
	}
}

func TestLeakScanAllowancesBindExactComponents(t *testing.T) {
	drive := func(user string) string {
		return "C:" + `\` + "Users" + `\` + user
	}
	unc := func(host, share string) string {
		return strings.Repeat(`\`, 2) + host + `\` + share
	}
	issue135ManifestHash := "B5E6DA1688FAEEB5EBCE4A2B2B7FF0FF" + "8B6BC8C3050C9B0990D8B6DAFEC13C66"
	issue135UntaggedExecutable := "untagged" + ".exe"
	issue135TaggedExecutable := "tagged" + ".exe"
	issue135Evidence := strings.Join([]string{
		issue135ManifestHash,
		"f7860ae8804b27954bf3" + "3708d16a92797b4d66f0",
		"409586B47D3D530C7A7FA816288E1851A" + "828858E4F52E857BF4A223FEFE26332",
		"4914E61763B02D073E7DB79771E0151B9" + "3BB6F2102C9F87C8A0CD91C130F76F9",
		"00FD27577869D8A26D94DA65A2C2FC2A" + "FE6810EDA04A720CC1150B68F859BCFF",
		issue135ManifestHash,
		"D7D79A86A64124349F28D833E6AB4AD6" + "53E612F407C8E837836B7C5871197754",
		"A0F5D1314A041DFAA5FAD2D01382F032" + "3CBEC39EA8E71FEE27D037A3C80B4769",
		"0EBF09387CD75986FEDFF1985ED35E0BE" + "4EEE65B24B85DD8A7846D1F4E47AF72",
		"1E7C90B35D797AC4751ED3599B7AB2DF" + "CDC23CBA88A9020BB1E564A08BF20E2A",
		"E8EA9CA6732A4FA226EE176FF6A0CEFF" + "D58B368C0BD5E80109966C516AD5E8D4",
		issue135UntaggedExecutable,
		issue135TaggedExecutable,
	}, "\n")
	cases := []struct {
		name      string
		files     map[string][]byte
		wantClean bool
	}{
		{"exact sanctioned values", map[string][]byte{
			"host/webview_windows_test.go": []byte(drive("jane") + "\n" + unc("jane", "share")),
		}, true},
		{"drive substring", map[string][]byte{
			"host/webview_windows_test.go": []byte(drive("notjane") + "\n" + unc("jane", "share")),
		}, false},
		{"UNC share substring", map[string][]byte{
			"host/webview_windows_test.go": []byte(drive("jane") + "\n" + unc("private", "jane")),
		}, false},
		{"second UNC share substring", map[string][]byte{
			"internal/doctor/probe_windows_test.go": []byte(unc("private", "server")),
		}, false},
		{"placeholder in UNC share", map[string][]byte{
			"private.txt": []byte(unc("PRIVATE-NAS", "<host>")),
		}, false},
		{"case-spoofed allowance path", map[string][]byte{
			"Host/webview_windows_test.go": []byte(drive("jane") + "\n" + unc("jane", "share")),
		}, false},
		{"same basename outside exact path", map[string][]byte{
			"nested/webview_windows_test.go": []byte(drive("jane") + "\n" + unc("jane", "share")),
		}, false},
		{"under consumed", map[string][]byte{
			"host/webview_windows_test.go": []byte(drive("jane")),
		}, false},
		{"over consumed", map[string][]byte{
			"host/webview_windows_test.go": []byte(drive("jane") + "\n" + drive("jane") + "\n" + unc("jane", "share")),
		}, false},
		{"exact Windows compatibility evidence", map[string][]byte{
			"docs/verification-records/2026-08.md": []byte(strings.Join([]string{
				"2a20cffb0dfdd4dc6b3af" + "028eed5f63e4955b1af",
				"5A9B807B7B809F666B2B3AD11D851" + "8B896B079EC3B5515317046B0796A424F00",
				"A6B15AD5DAE3D2BFDD0B5FC0D295" + "2A02234636AC71FA552CBAE379BD39B51860",
				"amd64v1" + ".exe",
			}, "\n")),
			"docs/windows-10-compatibility.md": []byte(strings.Repeat("app"+".exe\n", 4)),
		}, true},
		{"Windows compatibility evidence must match its exact capture", map[string][]byte{
			"docs/verification-records/2026-08.md": []byte(strings.Join([]string{
				"5A9B807B7B809F666B2B3AD11D851" + "8B896B079EC3B5515317046B0796A424F01",
			}, "\n")),
		}, false},
		{"Windows compatibility executable capture is under counted", map[string][]byte{
			"docs/windows-10-compatibility.md": []byte(strings.Repeat("app"+".exe\n", 3)),
		}, false},
		{"Windows compatibility executable capture is over counted", map[string][]byte{
			"docs/windows-10-compatibility.md": []byte(strings.Repeat("app"+".exe\n", 5)),
		}, false},
		{"exact Issue 135 evidence", map[string][]byte{
			"docs/issue-135-paired-live-verification.md": []byte(issue135Evidence),
		}, true},
		{"Issue 135 manifest hash is under counted", map[string][]byte{
			"docs/issue-135-paired-live-verification.md": []byte(strings.Replace(issue135Evidence, issue135ManifestHash+"\n", "", 1)),
		}, false},
		{"Issue 135 manifest hash is over counted", map[string][]byte{
			"docs/issue-135-paired-live-verification.md": []byte(issue135Evidence + "\n" + issue135ManifestHash),
		}, false},
		{"Issue 135 executable capture is under counted", map[string][]byte{
			"docs/issue-135-paired-live-verification.md": []byte(strings.Replace(issue135Evidence, issue135UntaggedExecutable+"\n", "", 1)),
		}, false},
		{"Issue 135 executable capture is over counted", map[string][]byte{
			"docs/issue-135-paired-live-verification.md": []byte(issue135Evidence + "\n" + issue135TaggedExecutable),
		}, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := runLeakScan(t, newLeakScanRepository(t, testCase.files))
			if testCase.wantClean {
				if result.exitCode != 0 || !strings.Contains(result.output, "clean within configured scope") {
					t.Fatalf("exact sanctioned allowances were not clean (exit %d):\n%s", result.exitCode, result.output)
				}
				return
			}
			assertLeakScanFailed(t, result)
			for path := range testCase.files {
				if !strings.Contains(result.output, filepath.ToSlash(path)) {
					t.Errorf("allowance failure did not name %s:\n%s", path, result.output)
				}
			}
		})
	}
}

func TestLeakScanDoesNotApplyCommitAllowanceWithoutExactAnchor(t *testing.T) {
	marker := "C:/Users/" + "alice"
	cases := []struct {
		name  string
		files map[string][]byte
	}{
		{"missing", map[string][]byte{"clean.txt": []byte("clean\n")}},
		{"case spoofed", map[string][]byte{
			"Docs/decisions/0025-urls-are-logged-as-urls.md": []byte("clean\n"),
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := newLeakScanRepositoryWithMessage(t, testCase.files, marker)
			assertLeakScanFailed(t, runLeakScan(t, root))
		})
	}
}

// The scanner must bind Git's selected index/object source before reading files.
// A clean decoy GIT_DIR beside a leaky worktree reproduces the old false verdict.
func TestLeakScanRejectsRedirectedGitSource(t *testing.T) {
	root := newLeakScanRepository(t, map[string][]byte{
		"clean.txt": []byte("clean\n"),
		"leak.txt":  []byte("C:/Users/" + "private-user/secret\n"),
	})
	decoy := newLeakScanRepository(t, map[string][]byte{"clean.txt": []byte("clean\n")})
	result := runLeakScanWithEnv(t, root, map[string]string{
		"GIT_DIR": filepath.Join(decoy, ".git"),
	})
	assertLeakScanFailed(t, result)
}

// Replacement refs and grafts can hide a real reachable message while HEAD and
// ordinary log commands remain green; both fixtures drive the real script.
func TestLeakScanRejectsRewrittenHistory(t *testing.T) {
	marker := "C:/Users/" + "private-user"
	t.Run("replacement object", func(t *testing.T) {
		root := newLeakScanRepositoryWithMessage(t, map[string][]byte{"clean.txt": []byte("clean\n")}, marker)
		replacement := strings.TrimSpace(gitOutput(t, root, "commit-tree", "HEAD^{tree}", "-m", "clean replacement"))
		gitRun(t, root, "replace", "HEAD", replacement)
		assertLeakScanFailed(t, runLeakScan(t, root))
	})
	t.Run("legacy graft", func(t *testing.T) {
		root := newLeakScanRepositoryWithMessage(t, map[string][]byte{"clean.txt": []byte("clean\n")}, marker)
		writeLeakScanFiles(t, root, map[string][]byte{"clean.txt": []byte("clean again\n")})
		gitRun(t, root, "add", "-A")
		gitRun(t, root, "commit", "-q", "-m", "clean head")
		head := strings.TrimSpace(gitOutput(t, root, "rev-parse", "HEAD"))
		writeLeakScanFiles(t, root, map[string][]byte{".git/info/grafts": []byte(head + "\n")})
		assertLeakScanFailed(t, runLeakScan(t, root))
	})
}

func TestLeakScanHashAllowancesBindExactPins(t *testing.T) {
	checkout := "3d3c42e5aac5ba805825" + "da76410c181273ba90b1"
	setupGo := "b7ad1dad31e06c5925ef" + "5d2fc7ad053ef454303e"
	clean := strings.Join([]string{
		"\"jobs\":",
		"  \"first\":",
		"    \"steps\":",
		"      - uses: actions/checkout@" + checkout,
		"      - \"uses\": actions/setup-go@" + setupGo,
		"  second:",
		"    steps:",
		"      - name: checkout",
		"        \"uses\": actions/checkout@" + checkout,
		"      - name: setup",
		"        uses: actions/setup-go@" + setupGo,
		"  third:",
		"    steps:",
		"      - uses: actions/checkout@" + checkout,
		"      - uses: actions/setup-go@" + setupGo,
	}, "\n")
	explicitScalarDecoys := strings.Join([]string{
		"decoy: |4",
		"    steps:",
		"      - uses: actions/checkout@" + checkout,
		"      - uses: actions/setup-go@" + setupGo,
		"      - uses: actions/checkout@" + checkout,
		"      - uses: actions/setup-go@" + setupGo,
	}, "\n")
	commentDecoys := strings.Join([]string{
		"# jobs:",
		"#   first:",
		"#     steps:",
		"#       - uses: actions/checkout@" + checkout,
		"#       - uses: actions/setup-go@" + setupGo,
		"#       - uses: actions/checkout@" + checkout,
		"#       - uses: actions/setup-go@" + setupGo,
	}, "\n")
	unrelatedSteps := strings.Join([]string{
		"data:",
		"  first:",
		"    steps:",
		"      - uses: actions/checkout@" + checkout,
		"      - uses: actions/setup-go@" + setupGo,
		"      - uses: actions/checkout@" + checkout,
		"      - uses: actions/setup-go@" + setupGo,
	}, "\n")
	for _, testCase := range []struct {
		name      string
		workflow  string
		wantClean bool
	}{
		{"exact executable pins with quoted keys", clean, true},
		{"count-preserving substitution", strings.Replace(clean, "@"+checkout, "@v4", 1) + "\nartifact=" + checkout, false},
		{"explicit-indentation scalar cannot authorize pins", explicitScalarDecoys, false},
		{"comments cannot create executable pin context", commentDecoys, false},
		{"steps outside root jobs cannot authorize pins", unrelatedSteps, false},
	} {

		t.Run(testCase.name, func(t *testing.T) {
			result := runLeakScan(t, newLeakScanRepository(t, map[string][]byte{
				".github/workflows/ci.yml": []byte(testCase.workflow),
			}))
			if testCase.wantClean {
				if result.exitCode != 0 || !strings.Contains(result.output, "clean within configured scope") {
					t.Fatalf("exact pins were not clean (exit %d):\n%s", result.exitCode, result.output)
				}
				return
			}
			assertLeakScanFailed(t, result)
		})
	}
}

func TestWorkflowStepParserIgnoresScalarsAndNormalizesQuotedKeys(t *testing.T) {
	job := strings.Join([]string{
		"    name: |",
		"      - uses: actions/checkout@decoy",
		"      - uses: actions/setup-go@decoy",
		"    steps:",
		"      - \"uses\": actions/checkout@pinned",
		"      - 'uses': actions/setup-go@pinned",
		"      - \"name\": leak-scan",
		"        \"run\": pwsh scripts/leak-scan.ps1",
	}, "\n")
	steps := workflowStepBlocks(job)
	if len(steps) != 3 {
		t.Fatalf("workflow steps = %d, want 3 executable steps; scalar decoys were admitted", len(steps))
	}
	if !workflowStepUsesAction(steps[0], "actions/checkout", "pinned") ||
		!workflowStepUsesAction(steps[1], "actions/setup-go", "pinned") ||
		!workflowStepHasLine(steps[2], "name: leak-scan") ||
		!workflowStepHasLine(steps[2], "run: pwsh scripts/leak-scan.ps1") {
		t.Fatalf("quoted workflow mappings were not normalized: %#v", steps)
	}
}

func TestWorkflowStepHelpersRequireExecutableNesting(t *testing.T) {
	job := strings.Join([]string{
		"    steps:",
		"    # A comment at mapping indentation does not end the steps sequence.",
		"      # - uses: actions/checkout@comment",
		"      - uses: actions/checkout@shallow",
		"        env:",
		"          uses: actions/setup-go@nested",
		"          name: nested-name",
		"          run: nested-run",
		"          if: always()",
		"          continue-on-error: true",
		"          fetch-depth: 0",
		"        data:",
		"          with:",
		"            fetch-depth: 0",
		"      - uses: actions/checkout@pinned",
		"        with:",
		"          # fetch-depth: 0",
		"          fetch-depth: 0",
		"      - name: leak-scan",
		"        env:",
		"          run: pwsh scripts/leak-scan.ps1",
		"        # run: pwsh scripts/leak-scan.ps1",
		"        run: echo safe",
		"        if: success()",
		"        continue-on-error: false",
	}, "\n")
	steps := workflowStepBlocks(job)
	if len(steps) != 3 {
		t.Fatalf("workflow steps = %d, want 3; comments changed sequence structure", len(steps))
	}

	shallow := steps[0]
	if !workflowStepUsesAction(shallow, "actions/checkout", "shallow") ||
		workflowStepUsesAction(shallow, "actions/setup-go", "nested") ||
		workflowStepHasLine(shallow, "name: nested-name") ||
		workflowStepHasLine(shallow, "run: nested-run") ||
		workflowStepHasKey(shallow, "if") ||
		workflowStepHasKey(shallow, "continue-on-error") ||
		workflowStepHasChildLine(shallow, "with", "fetch-depth: 0") {
		t.Fatal("nested workflow data impersonated executable step configuration")
	}

	checkout := steps[1]
	if !workflowStepUsesAction(checkout, "actions/checkout", "pinned") ||
		!workflowStepHasChildLine(checkout, "with", "fetch-depth: 0") {
		t.Fatal("step-level action or direct with input was not recognized")
	}

	scan := steps[2]
	if workflowStepHasLine(scan, "run: pwsh scripts/leak-scan.ps1") ||
		!workflowStepHasLine(scan, "name: leak-scan") ||
		!workflowStepHasLine(scan, "run: echo safe") ||
		!workflowStepHasKey(scan, "if") ||
		!workflowStepHasKey(scan, "continue-on-error") {
		t.Fatal("comments or indentation changed step-level mapping authority")
	}
}

// Parse actual job and step blocks. Repository-wide token searches stay green if
// pins move after the scan, depth moves jobs, checkout targets another source,
// commands survive only in comments, or a scope gains a disabling condition.
func TestLeakScanWorkflowKeepsFullHistoryGate(t *testing.T) {
	workflow := currentLeakScanWorkflow(t)
	windows := workflowJobBlock(t, workflow, "windows")
	if workflowJobHasKey(windows, "if") || workflowJobHasKey(windows, "continue-on-error") {
		t.Fatal("Windows job is conditional or non-fatal")
	}
	checkoutPin := "3d3c42e5aac5ba805825" + "da76410c181273ba90b1"
	setupGoPin := "b7ad1dad31e06c5925ef" + "5d2fc7ad053ef454303e"
	var checkouts, setups, scans []int
	steps := workflowStepBlocks(windows)
	for index, step := range steps {
		switch {
		case workflowStepHasPrefix(step, "actions/checkout@"):
			checkouts = append(checkouts, index)
		case workflowStepHasPrefix(step, "actions/setup-go@"):
			setups = append(setups, index)
		case workflowStepHasLine(step, "name: leak-scan"):
			scans = append(scans, index)
		}
	}
	if len(checkouts) != 1 || len(setups) != 1 || len(scans) != 1 {
		t.Fatalf("Windows workflow authority steps: checkout=%d setup-go=%d leak-scan=%d; want exactly one each", len(checkouts), len(setups), len(scans))
	}
	checkout, setup, scan := steps[checkouts[0]], steps[setups[0]], steps[scans[0]]
	if !workflowStepUsesAction(checkout, "actions/checkout", checkoutPin) ||
		!workflowStepHasChildLine(checkout, "with", "fetch-depth: 0") {
		t.Fatal("Windows checkout no longer uses the allowed pin with full history")
	}
	if !workflowStepUsesAction(setup, "actions/setup-go", setupGoPin) {
		t.Fatal("Windows setup-go no longer uses the allowed pin")
	}
	for _, override := range []string{"ref", "repository", "path"} {
		if workflowStepHasChildKey(checkout, "with", override) {
			t.Fatalf("Windows checkout overrides %s and can scan the wrong source:\n%s", override, checkout)
		}
	}
	if !workflowStepHasLine(scan, "run: pwsh scripts/leak-scan.ps1") {
		t.Fatal("Windows workflow no longer has an executable repository leak-scan step")
	}
	if checkouts[0] >= setups[0] || setups[0] >= scans[0] {
		t.Fatalf("Windows authority order checkout=%d setup-go=%d leak-scan=%d; scan must use the pinned checkout", checkouts[0], setups[0], scans[0])
	}
	for name, step := range map[string]string{"checkout": checkout, "setup-go": setup, "leak-scan": scan} {
		if workflowStepHasKey(step, "if") || workflowStepHasKey(step, "continue-on-error") {
			t.Fatalf("Windows %s step is conditional or non-fatal:\n%s", name, step)
		}
	}
}

// TestCIWorkflowRestrictsTokenToContentsRead keeps the workflow token from
// silently regaining GitHub's default write grants (issue #13).
func TestCIWorkflowRestrictsTokenToContentsRead(t *testing.T) {
	workflow := currentLeakScanWorkflow(t)
	if !strings.Contains(workflow, "permissions:\n  contents: read\n\njobs:") {
		t.Fatal("CI workflow must declare top-level permissions: contents: read before jobs")
	}
}

// The two existing Go-version matrices expand to four jobs. This unconditional
// singleton is the fifth and names the only supported process ABI directly.
func TestCIWorkflowKeepsExplicitWindowsX64Lane(t *testing.T) {
	workflow := currentLeakScanWorkflow(t)
	x64 := workflowJobBlock(t, workflow, "windows-x64")
	for _, key := range []string{"if", "continue-on-error", "strategy"} {
		if workflowJobHasKey(x64, key) {
			t.Fatalf("Windows/x64 job has forbidden %s authority", key)
		}
	}
	for _, line := range []string{"name: windows x64", "runs-on: windows-latest"} {
		if !workflowJobHasLine(x64, line) {
			t.Fatalf("Windows/x64 job lost %q", line)
		}
	}
	for _, line := range []string{"GOOS: windows", "GOARCH: amd64", `MULLION_REQUIRE_WEBVIEW2: "1"`} {
		if !workflowJobHasChildLine(x64, "env", line) {
			t.Fatalf("Windows/x64 job lost environment contract %q", line)
		}
	}

	checkoutPin := "3d3c42e5aac5ba805825" + "da76410c181273ba90b1"
	setupGoPin := "b7ad1dad31e06c5925ef" + "5d2fc7ad053ef454303e"
	var checkouts, setups, builds, tests []string
	for _, step := range workflowStepBlocks(x64) {
		switch {
		case workflowStepHasPrefix(step, "actions/checkout@"):
			checkouts = append(checkouts, step)
		case workflowStepHasPrefix(step, "actions/setup-go@"):
			setups = append(setups, step)
		case workflowStepHasLine(step, "name: build Windows/x64"):
			builds = append(builds, step)
		case workflowStepHasLine(step, "name: test Windows/x64"):
			tests = append(tests, step)
		}
	}
	if len(checkouts) != 1 || len(setups) != 1 || len(builds) != 1 || len(tests) != 1 {
		t.Fatalf("Windows/x64 steps: checkout=%d setup=%d build=%d test=%d; want one each",
			len(checkouts), len(setups), len(builds), len(tests))
	}
	if !workflowStepUsesAction(checkouts[0], "actions/checkout", checkoutPin) ||
		!workflowStepUsesAction(setups[0], "actions/setup-go", setupGoPin) ||
		!workflowStepHasChildLine(setups[0], "with", "go-version: stable") {
		t.Fatal("Windows/x64 action pins or stable Go selection changed")
	}
	if !workflowStepHasLine(builds[0], "run: go build ./...") ||
		!workflowStepHasLine(tests[0], "run: go test -count=1 ./...") {
		t.Fatal("Windows/x64 build or uncached full-suite command changed")
	}
	for _, step := range []string{checkouts[0], setups[0], builds[0], tests[0]} {
		if workflowStepHasKey(step, "if") || workflowStepHasKey(step, "continue-on-error") {
			t.Fatal("Windows/x64 authority step became conditional or non-fatal")
		}
	}
}

func TestWorkflowJobBlockIgnoresBlankAndCommentDelimiters(t *testing.T) {
	workflow := currentLeakScanWorkflow(t)
	const nextJob = "\n  windows-x64:"
	mutation := "\n  # Comments and blank lines do not end the windows mapping.\n\n    continue-on-error: true" + nextJob
	mutated := strings.Replace(workflow, nextJob, mutation, 1)
	if mutated == workflow {
		t.Fatal("current workflow fixture no longer has the Windows/x64 job boundary")
	}
	windows := workflowJobBlock(t, mutated, "windows")
	if !workflowJobHasKey(windows, "continue-on-error") {
		t.Fatal("comment-delimited current-workflow mutation escaped the windows job block")
	}
}

func currentLeakScanWorkflow(t *testing.T) string {
	t.Helper()
	workflow, err := os.ReadFile(filepath.Join(moduleRoot(t), ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	return string(workflow)
}

func workflowJobBlock(t *testing.T, workflow, name string) string {
	t.Helper()
	lines := strings.Split(workflow, "\n")
	start := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line == "  "+name+":" {
			start = index + 1
			continue
		}
		if start >= 0 && strings.HasPrefix(line, "  ") && len(line) > 2 && line[2] != ' ' {
			return strings.Join(lines[start:index], "\n")
		}
	}
	if start < 0 {
		t.Fatalf("workflow job %q not found", name)
	}
	return strings.Join(lines[start:], "\n")
}

func workflowJobHasKey(job, want string) bool {
	for _, line := range strings.Split(job, "\n") {
		if strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "      ") &&
			workflowMappingKey(strings.TrimSpace(line)) == want {
			return true
		}
	}
	return false
}

func workflowJobHasLine(job, want string) bool {
	wantKey, wantValue, ok := workflowMapping(strings.TrimSpace(want))
	if !ok {
		return false
	}
	for _, line := range strings.Split(job, "\n") {
		if workflowLineIndent(line) != 4 {
			continue
		}
		key, value, ok := workflowMapping(strings.TrimSpace(line))
		if ok && key == wantKey && value == wantValue {
			return true
		}
	}
	return false
}

func workflowJobHasChildLine(job, parent, want string) bool {
	wantKey, wantValue, ok := workflowMapping(strings.TrimSpace(want))
	if !ok {
		return false
	}
	return workflowChildMappingMatches(job, 4, parent, wantKey, wantValue, true)
}

func workflowStepBlocks(job string) []string {
	var steps []string
	var current []string
	inSteps := false
	for _, line := range strings.Split(job, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "      ") {
			inSteps = workflowMappingKey(trimmed) == "steps"
			continue
		}
		if !inSteps {
			continue
		}
		if strings.HasPrefix(line, "      - ") {
			if len(current) > 0 {
				steps = append(steps, strings.Join(current, "\n"))
			}
			current = current[:0]
		}
		if len(current) > 0 || strings.HasPrefix(line, "      - ") {
			current = append(current, line)
		}
	}
	if len(current) > 0 {
		steps = append(steps, strings.Join(current, "\n"))
	}
	return steps
}

func workflowStepHasLine(step, want string) bool {
	wantKey, wantValue, ok := workflowMapping(strings.TrimSpace(want))
	if !ok {
		return false
	}
	mappingIndent := workflowStepMappingIndent(step)
	for _, line := range strings.Split(step, "\n") {
		key, value, ok := workflowStepMappingAtIndent(line, mappingIndent)
		if ok && key == wantKey && value == wantValue {
			return true
		}
	}
	return false
}

func workflowStepHasPrefix(step, want string) bool {
	mappingIndent := workflowStepMappingIndent(step)
	for _, line := range strings.Split(step, "\n") {
		key, value, ok := workflowStepMappingAtIndent(line, mappingIndent)
		if ok && key == "uses" && strings.HasPrefix(value, want) {
			return true
		}
	}
	return false
}

func workflowStepUsesAction(step, action, pin string) bool {
	mappingIndent := workflowStepMappingIndent(step)
	for _, line := range strings.Split(step, "\n") {
		key, value, ok := workflowStepMappingAtIndent(line, mappingIndent)
		if ok && key == "uses" && value == action+"@"+pin {
			return true
		}
	}
	return false
}

func workflowStepHasKey(step, want string) bool {
	mappingIndent := workflowStepMappingIndent(step)
	for _, line := range strings.Split(step, "\n") {
		key, _, ok := workflowStepMappingAtIndent(line, mappingIndent)
		if ok && key == want {
			return true
		}
	}
	return false
}

func workflowStepHasChildLine(step, parent, want string) bool {
	wantKey, wantValue, ok := workflowMapping(strings.TrimSpace(want))
	if !ok {
		return false
	}
	return workflowStepChildMappingMatches(step, parent, wantKey, wantValue, true)
}

func workflowStepHasChildKey(step, parent, want string) bool {
	return workflowStepChildMappingMatches(step, parent, want, "", false)
}

func workflowStepChildMappingMatches(step, parent, wantKey, wantValue string, matchValue bool) bool {
	return workflowChildMappingMatches(step, workflowStepMappingIndent(step), parent, wantKey, wantValue, matchValue)
}

func workflowChildMappingMatches(text string, mappingIndent int, parent, wantKey, wantValue string, matchValue bool) bool {
	parentActive := false
	childIndent := -1
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if workflowLineIndent(line) == mappingIndent {
			key, value, ok := workflowMapping(trimmed)
			parentActive = ok && key == parent && value == ""
			childIndent = -1
			continue
		}
		if !parentActive {
			continue
		}
		indent := workflowLineIndent(line)
		if indent <= mappingIndent {
			parentActive = false
			continue
		}
		if childIndent < 0 {
			childIndent = indent
		}
		if indent != childIndent {
			continue
		}
		key, value, ok := workflowMapping(trimmed)
		if ok && key == wantKey && (!matchValue || value == wantValue) {
			return true
		}
	}
	return false
}

func workflowStepMappingIndent(step string) int {
	for _, line := range strings.Split(step, "\n") {
		indent := workflowLineIndent(line)
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "- ") {
			return indent + 2
		}
	}
	return -1
}

func workflowStepMappingAtIndent(line string, mappingIndent int) (string, string, bool) {
	indent := workflowLineIndent(line)
	trimmed := strings.TrimLeft(line, " ")
	if strings.HasPrefix(trimmed, "- ") {
		if indent+2 != mappingIndent {
			return "", "", false
		}
		trimmed = strings.TrimPrefix(trimmed, "- ")
	} else if indent != mappingIndent {
		return "", "", false
	}
	if strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	return workflowMapping(trimmed)
}

func workflowLineIndent(line string) int {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	return indent
}

func workflowMappingKey(line string) string {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(line[:colon]), `"'`)
}

func workflowMapping(line string) (string, string, bool) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return "", "", false
	}
	key := strings.Trim(strings.TrimSpace(line[:colon]), `"'`)
	value := strings.TrimSpace(line[colon+1:])
	if comment := strings.Index(value, " #"); comment >= 0 {
		value = strings.TrimSpace(value[:comment])
	}
	return key, value, true
}

func newLeakScanRepository(t *testing.T, files map[string][]byte) string {
	t.Helper()
	return newLeakScanRepositoryWithMessage(t, files, "clean fixture")
}

func newLeakScanRepositoryWithMessage(t *testing.T, files map[string][]byte, message string) string {
	t.Helper()
	root := t.TempDir()
	copyLeakScanScript(t, root)
	writeLeakScanFiles(t, root, files)
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", message)
	return root
}

func copyLeakScanScript(t *testing.T, root string) {
	t.Helper()
	script, err := os.ReadFile(filepath.Join(moduleRoot(t), "scripts", "leak-scan.ps1"))
	if err != nil {
		t.Fatalf("read leak-scan.ps1: %v", err)
	}
	writeLeakScanFiles(t, root, map[string][]byte{"scripts/leak-scan.ps1": script})
}

func writeLeakScanFiles(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

func gitInit(t *testing.T, root string) {
	t.Helper()
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "config", "core.quotePath", "true")
	gitRun(t, root, "config", "core.autocrlf", "false")
	gitRun(t, root, "config", "core.safecrlf", "false")
	gitRun(t, root, "config", "user.name", "t")
	gitRun(t, root, "config", "user.email", "t@t")
}

func gitRun(t *testing.T, root string, args ...string) {
	t.Helper()
	_ = gitOutput(t, root, args...)
}

func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	cmd := exec.Command(git, args...)
	if root != "" {
		cmd.Dir = root
	}
	hideChildConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func runLeakScan(t *testing.T, root string) leakScanResult {
	t.Helper()
	return runLeakScanWithEnv(t, root, nil)
}

func runLeakScanWithEnv(t *testing.T, root string, environment map[string]string) leakScanResult {
	t.Helper()
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh not on PATH; leak-scan is locked where PowerShell is available")
	}
	cmd := exec.Command(pwsh, "-NoProfile", "-File", filepath.Join(root, "scripts", "leak-scan.ps1"))
	cmd.Dir = root
	cmd.Env = os.Environ()
	for name, value := range environment {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	hideChildConsole(cmd)
	out, err := cmd.CombinedOutput()
	result := leakScanResult{output: string(out)}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run leak-scan.ps1: %v\n%s", err, out)
	}
	return result
}

func assertLeakScanFailed(t *testing.T, result leakScanResult) {
	t.Helper()
	if result.exitCode == 0 {
		t.Fatalf("leak-scan returned success:\n%s", result.output)
	}
	if strings.Contains(result.output, "clean within configured scope") {
		t.Fatalf("failing leak-scan printed the clean verdict:\n%s", result.output)
	}
}
