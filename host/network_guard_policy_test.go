package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func joined(parts ...string) string { return strings.Join(parts, "") }

const networkGuardReadFailureEnv = "MULLION_NETWORK_GUARD_FAIL_READ"

// TestMain installs the selected-file fault before the child runs the real
// TestNoNetworkListener entrypoint. The ordinary suite leaves os.ReadFile intact.
func TestMain(m *testing.M) {
	if target := os.Getenv(networkGuardReadFailureEnv); target != "" {
		networkPolicyReadFile = func(path string) ([]byte, error) {
			if strings.EqualFold(filepath.Clean(path), filepath.Clean(target)) {
				return nil, &os.PathError{Op: "injected selected-file read", Path: path, Err: os.ErrPermission}
			}
			return os.ReadFile(path)
		}
	}
	os.Exit(m.Run())
}

func TestStripExemptNameKeepsAddressForms(t *testing.T) {
	name := joined("mullion.", "local", "host")
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"bare name", name, ""},
		{"origin", "https://" + name + "/index.html", "https:///index.html"},
		{"port", name + ":8080", name + ":8080"},
		{"subdomain", "preview." + name, "preview." + name},
		{"trailing dot", name + ".", name + "."},
		{"userinfo", name + "@example.invalid", name + "@example.invalid"},
		{"suffix label", name + ".example.invalid", name + ".example.invalid"},
		{"percent escape", name + "%3A8080", name + "%3A8080"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := stripExemptName(testCase.source, name); got != testCase.want {
				t.Fatalf("stripExemptName(%q) = %q, want %q", testCase.source, got, testCase.want)
			}
		})
	}
}

func TestNetworkGuardPinsTheInterceptedVirtualHost(t *testing.T) {
	guardName := joined("mullion.", "local", "host")
	if defaultVirtualHost != guardName {
		t.Fatalf("defaultVirtualHost is %q but the network guard exempts %q; changing this name requires revisiting decision 0030", defaultVirtualHost, guardName)
	}
}

func TestNetworkPolicyRecognizesAPIsWithoutSubstringFalsePositives(t *testing.T) {
	cases := []struct {
		name        string
		source      string
		wantFinding bool
	}{
		{"receiver server", "package probe\nimport \"net/http\"\nvar _ = &http.Server{}\n", true},
		{"inline server", "package probe\nimport h \"net/http\"\nvar _ = (&h.Server{}).ListenAndServe\n", true},
		{"file listener", "package probe\nimport n \"net\"\nvar _ = n.FileListener\n", true},
		{"aliased listen", "package probe\nimport n \"net\"\nvar _ = n.Listen\n", true},
		{"dot import", "package probe\nimport . \"net\"\nvar _ = Listen\n", true},
		{"function value", "package probe\nimport \"net\"\nvar f = net.Listen\n", true},
		{"listen config", "package probe\nimport \"net\"\nvar _ net.ListenConfig\n", true},
		{"test server", "package probe\nimport \"net/http/httptest\"\nvar _ = httptest.NewServer\n", true},
		{"syscall must load Winsock", "package probe\nimport \"syscall\"\nvar _ = syscall.MustLoadDLL(\"ws2_32.dll\")\n", true},
		{"extensionless syscall Winsock", "package probe\nimport \"syscall\"\nvar _, _ = syscall.LoadLibrary(\"ws2_32\")\n", true},
		{"windows load library", "package probe\nimport w \"golang.org/x/sys/windows\"\nvar _, _ = w.LoadLibrary(\"ws2_32.dll\")\n", true},
		{"windows load library ex", "package probe\nimport w \"golang.org/x/sys/windows\"\nvar _, _ = w.LoadLibraryEx(\"ws2_32.dll\", 0, 0)\n", true},
		{"constant Winsock name", "package probe\nimport w \"golang.org/x/sys/windows\"\nconst winsock = \"ws2_\" + \"32.dll\"\nvar _ = w.NewLazySystemDLL(winsock)\n", true},
		{"captured Winsock loader", "package probe\nimport w \"golang.org/x/sys/windows\"\nvar load = w.NewLazySystemDLL\nvar _ = load(\"ws2_32.dll\")\n", true},
		{"captured loader alias", "package probe\nimport w \"golang.org/x/sys/windows\"\nvar load = w.NewLazySystemDLL\nvar alias = load\nvar _ = alias(\"ws2_32.dll\")\n", true},
		{"captured loader before reassignment", "package probe\nimport w \"golang.org/x/sys/windows\"\nvar load = w.NewLazySystemDLL\nfunc f() { _ = load(\"ws2_32.dll\"); load = nil }\n", true},
		{"converted captured loader", "package probe\nimport w \"golang.org/x/sys/windows\"\ntype loader func(string) *w.LazyDLL\nvar load = loader(w.NewLazySystemDLL)\nvar _ = load(\"ws2_32.dll\")\n", true},
		{"unnamed function conversion", "package probe\nimport w \"golang.org/x/sys/windows\"\nvar load = (func(string) *w.LazyDLL)(w.NewLazySystemDLL)\nvar _ = load(\"ws2_32.dll\")\n", true},
		{"parenthesized captured-loader assignment", "package probe\nimport w \"golang.org/x/sys/windows\"\nvar load func(string) *w.LazyDLL\nfunc f() { (load) = w.NewLazySystemDLL; _ = load(\"ws2_32.dll\") }\n", true},
		{"generic captured loader conversion", "package probe\nimport w \"golang.org/x/sys/windows\"\ntype loader[T any] func(string) *w.LazyDLL\nvar load = loader[int](w.NewLazySystemDLL)\nvar _ = load(\"ws2_32.dll\")\n", true},
		{"multi-argument generic captured loader conversion", "package probe\nimport w \"golang.org/x/sys/windows\"\ntype loader[A, B any] func(string) *w.LazyDLL\nvar load = loader[int, string](w.NewLazySystemDLL)\nvar _ = load(\"ws2_32.dll\")\n", true},
		{"windows LazyDLL literal", "package probe\nimport w \"golang.org/x/sys/windows\"\nvar _ = w.LazyDLL{Name: \"ws2_32.dll\"}\n", true},
		{"syscall LazyDLL literal", "package probe\nimport \"syscall\"\nvar _ = syscall.LazyDLL{Name: \"ws2_32.dll\"}\n", true},
		{"aliased Windows LazyDLL literal", "package probe\nimport w \"golang.org/x/sys/windows\"\ntype socketDLL = w.LazyDLL\nvar _ = socketDLL{Name: \"ws2_32.dll\"}\n", true},
		{"generic Windows LazyDLL alias", "package probe\nimport w \"golang.org/x/sys/windows\"\ntype socketDLL[T any] = w.LazyDLL\nvar _ = socketDLL[int]{Name: \"ws2_32.dll\"}\n", true},
		{"multi-argument generic Windows LazyDLL alias", "package probe\nimport w \"golang.org/x/sys/windows\"\ntype socketDLL[A, B any] = w.LazyDLL\nvar _ = socketDLL[int, string]{Name: \"ws2_32.dll\"}\n", true},
		{"parenthesized string conversion", "package probe\nimport w \"golang.org/x/sys/windows\"\nvar _ = w.NewLazySystemDLL((string)(\"ws2_32.dll\"))\n", true},
		{"string-converted Winsock constant", "package probe\nimport w \"golang.org/x/sys/windows\"\nconst winsock = string(\"ws2_32.dll\")\nvar _ = w.NewLazySystemDLL(winsock)\n", true},
		{"named string alias conversion", "package probe\nimport w \"golang.org/x/sys/windows\"\ntype dllName = string\nvar _ = w.NewLazySystemDLL(dllName(\"ws2_32.dll\"))\n", true},
		{"generic string alias conversion", "package probe\nimport w \"golang.org/x/sys/windows\"\ntype dllName[T any] = string\nvar _ = w.NewLazySystemDLL(dllName[int](\"ws2_32.dll\"))\n", true},
		{"recorder", "package probe\nimport \"net/http/httptest\"\nvar _ = httptest.NewRecorder\n", false},
		{"listener type", "package probe\nimport \"net\"\nfunc accept(ln net.Listener) { _ = ln }\n", false},
		{"unrelated serve", "package probe\ntype service struct{}\nfunc (service) Serve() {}\n", false},
		{"http client", "package probe\nimport \"net/http\"\nvar _ http.Client\n", false},
		{"shadowed package name", "package probe\nimport \"net\"\nvar _ = net.IPv4len\ntype service struct{}\nfunc (service) Listen() {}\nfunc f() { net := service{}; net.Listen() }\n", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			findings, err := scanGoNetworkPolicy("probe.go", []byte(testCase.source))
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			if got := len(findings) > 0; got != testCase.wantFinding {
				t.Fatalf("finding = %v, want %v: %#v", got, testCase.wantFinding, findings)
			}
		})
	}
}

func TestNetworkPolicyLocksEveryDirectWinsockLoader(t *testing.T) {
	loaders := []struct {
		importPath string
		symbol     string
	}{
		{"syscall", "LoadDLL"},
		{"syscall", "LoadLibrary"},
		{"syscall", "MustLoadDLL"},
		{"syscall", "NewLazyDLL"},
		{"golang.org/x/sys/windows", "LoadDLL"},
		{"golang.org/x/sys/windows", "LoadLibrary"},
		{"golang.org/x/sys/windows", "LoadLibraryEx"},
		{"golang.org/x/sys/windows", "MustLoadDLL"},
		{"golang.org/x/sys/windows", "NewLazyDLL"},
		{"golang.org/x/sys/windows", "NewLazySystemDLL"},
	}
	for _, loader := range loaders {
		t.Run(filepath.Base(loader.importPath)+"_"+loader.symbol, func(t *testing.T) {
			if !winsockLoaders[loader.importPath][loader.symbol] {
				t.Fatalf("%s.%s disappeared from the policy table", loader.importPath, loader.symbol)
			}
			source := "package probe\nimport p \"" + loader.importPath + "\"\nvar _ = p." + loader.symbol + "(\"ws2_32.dll\")\n"
			findings, err := scanGoNetworkPolicy("probe.go", []byte(source))
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			if len(findings) == 0 {
				t.Fatalf("%s.%s produced no finding", loader.importPath, loader.symbol)
			}
		})
	}
}

func TestNetworkPolicyRecognizesEndpointFamilies(t *testing.T) {
	cases := []string{
		joined("http://Local", "Host:8080/"),
		joined("http://127", ".", "0.0.2:8080/"),
		joined("http://127", ".", "1:8080/"),
		joined("http://213070", "6433:8080/"),
		joined("http://0x7f", ".0.0.1:8080/"),
		joined("http://0177", ".0.0.1:8080/"),
		joined("http://127", ".0x1:8080/"),
		joined("http://127", ".01:8080/"),
		joined("http://0x7f", ".0x0.00.0x1:8080/"),
		joined("http://127", ".1.:8080/"),
		joined("http:/", "/0x0.00.0x0.0:8080/"),
		joined("http:/", "/00.0.:8080/"),
		joined("http:/", "/0x7f.0x.0x.0x1:8080/"),
		joined("http:/", "/0x/:8080/"),
		joined("http://[::", "1]:8080/"),
		joined("http://[0:0:0:0:0:0:0:", "1]:8080/"),
		joined("http://[::ffff:127.", "0.0.1]:8080/"),
		joined("http://[::ffff:0.", "0.0.0]:8080/"),
		joined("http:/", "/0.0.0.0:8080/"),
		joined("http:///", "/127.0.0.1:8080/"),
		joined("http:///", "/localhost:8080/"),
		joined("http:\\\\", "\\\\127.0.0.1:8080/"),
		joined("//127", ".1:8080/"),
		joined("//user@127", ".1:8080/"),
		joined(`fetch("//127`, `.1:8080/")`),
		joined(`const host = "127`, `.0.0.1:8080"`),
		joined("127", ".1:8080"),
		joined("http://127", ".0.0.1.:8080/"),
		joined("//[::", "1]:8080/"),
		joined(`const prior = "https://example.invalid/"; const endpoint = "local`, `host:8080";`),
		joined("http:/", "/user@0.0.0.0:8080/"),
		joined("http://user@127", ".1:8080/"),
		joined("http://[::", "]:8080/"),
		joined("https://preview.mullion.", "local", "host/"),
		joined("https://mullion.", "local", "host:8080/"),
		joined("https://mullion.", "local", "host%3A8080/"),
		joined("https://user@mullion.", "local", "host/"),
		joined("https://mullion.", "local", "host.example/"),
	}
	for _, value := range cases {
		if got := endpointPolicyFinding(value); got == "" {
			t.Errorf("endpointPolicyFinding(%q) returned no finding", value)
		}
	}
	for _, clean := range []string{
		joined("https://mullion.", "local", "host/index.html"),
		joined("https://MULLION.", "LOCAL", "HOST/index.html"),
		"https://example.invalid/path",
		"https://127.example.com/",
		"version 127.4",
		"http://127.1@example.invalid/",
		"http://0.0.0.0@example.invalid/",
		"http://example.invalid/path/127.1",
		"https://notlocalhost.example/",
		"https://localhostish.example/",
		"http://127.0.0.1@example.invalid/",
		"https://example.invalid/path/127.0.0.1",
		"http://2130706433@example.invalid/",
		"https://example.invalid/path/2130706433",
		"https://example.invalid/path/[::1]",
		"http://[::1]@example.invalid/",
		"0.0.0.0",
		"C:relative/path",
		"http://127.0xg:8080/",
		"http://127.08:8080/",
		"http://127.0x1000000:8080/",
		"http://127.1.example.invalid/",
		"http://0x7f.0x1.example.invalid/",
		"http://127.1%2eexample.com/",
		"http:////example.invalid/",
	} {
		if got := endpointPolicyFinding(clean); got != "" {
			t.Errorf("endpointPolicyFinding(%q) = %q, want clean", clean, got)
		}
	}
}

func TestEndpointAllowancesDoNotMaskSiblingFindings(t *testing.T) {
	allowed := joined("http://[::", "1]:8080/")
	mixed := joined("http://127", ".0.0.1/?reference=[::", "1]")
	for _, rel := range []string{"host/errorpage_test.go", "host/systembrowser_windows_test.go"} {
		if !endpointFindingAllowed(rel, allowed) {
			t.Errorf("%s no longer allows its bracketed loopback fixture", rel)
		}
		if endpointFindingAllowed(rel, mixed) {
			t.Errorf("%s allowed a forbidden sibling endpoint in %q", rel, mixed)
		}
	}
}

func TestNetworkPolicyFailsOnMalformedSelectedGoSource(t *testing.T) {
	root := writeNetworkFixture(t, map[string]string{"broken.go": "package"})
	if _, err := scanNetworkPolicy(root); err == nil {
		t.Fatal("malformed selected Go source produced a clean inspection")
	}
}

func TestNoNetworkListenerExercisesRealTraversalAndVerdict(t *testing.T) {
	endpoint := joined("http://127", ".", "0.0.1:8080/api")
	cases := []struct {
		name        string
		files       map[string]string
		wantBad     bool
		readFailure string
	}{
		{"clean", map[string]string{"clean.go": "package probe\nvar Message = \"clean\"\n"}, false, ""},
		{"listener", map[string]string{"probe.go": "package probe\nimport \"net\"\nvar _ = net.Listen\n"}, true, ""},
		{"shipped scheme-relative endpoint", map[string]string{"frontend/app.js": "fetch(\"//" + endpoint[len("http://"):] + "\")\n"}, true, ""},
		{"spoofed guard basename", map[string]string{"nested/leak_test.go": "package probe\nimport \"net\"\nvar _ = net.Listen\n"}, true, ""},
		{"spoofed loopback basename", map[string]string{"nested/loopback.go": "package probe\nconst endpoint = \"" + endpoint + "\"\n"}, true, ""},
		{"cross-file Winsock constant", map[string]string{
			"name.go": "package probe\nconst winsock = \"ws2_\" + \"32.dll\"\n",
			"load.go": "package probe\nimport w \"golang.org/x/sys/windows\"\nvar _ = w.NewLazySystemDLL(winsock)\n",
		}, true, ""},
		{"cross-file captured Winsock loader", map[string]string{
			"loader.go": "package probe\nimport w \"golang.org/x/sys/windows\"\nvar load = w.NewLazySystemDLL\n",
			"call.go":   "package probe\nvar _ = load(\"ws2_32.dll\")\n",
		}, true, ""},
		{"cross-file assigned Winsock loader", map[string]string{
			"declaration.go": "package probe\nimport w \"golang.org/x/sys/windows\"\nvar load func(string) *w.LazyDLL\n",
			"assignment.go":  "package probe\nimport w \"golang.org/x/sys/windows\"\nfunc f() { load = w.NewLazySystemDLL; _ = load(\"ws2_32.dll\") }\n",
		}, true, ""},
		{"same-file package assignment with sibling call", map[string]string{
			"binding.go": "package probe\nimport w \"golang.org/x/sys/windows\"\nvar load func(string) *w.LazyDLL\nfunc bind() { load = w.NewLazySystemDLL }\n",
			"call.go":    "package probe\nvar _ = load(\"ws2_32.dll\")\n",
		}, true, ""},
		{"cross-file parenthesized assigned Winsock loader", map[string]string{
			"declaration.go": "package probe\nimport w \"golang.org/x/sys/windows\"\nvar load func(string) *w.LazyDLL\n",
			"assignment.go":  "package probe\nimport w \"golang.org/x/sys/windows\"\nfunc f() { (load) = w.NewLazySystemDLL; _ = load(\"ws2_32.dll\") }\n",
		}, true, ""},
		{"cross-file loader type alias", map[string]string{
			"type.go": "package probe\nimport w \"golang.org/x/sys/windows\"\ntype loader func(string) *w.LazyDLL\n",
			"load.go": "package probe\nimport w \"golang.org/x/sys/windows\"\nvar load = loader(w.NewLazySystemDLL)\nvar _ = load(\"ws2_32.dll\")\n",
		}, true, ""},
		{"cross-file generic loader type", map[string]string{
			"type.go": "package probe\nimport w \"golang.org/x/sys/windows\"\ntype loader[T any] func(string) *w.LazyDLL\n",
			"load.go": "package probe\nimport w \"golang.org/x/sys/windows\"\nvar load = loader[int](w.NewLazySystemDLL)\nvar _ = load(\"ws2_32.dll\")\n",
		}, true, ""},
		{"cross-file multi-argument generic loader type", map[string]string{
			"type.go": "package probe\nimport w \"golang.org/x/sys/windows\"\ntype loader[A, B any] func(string) *w.LazyDLL\n",
			"load.go": "package probe\nimport w \"golang.org/x/sys/windows\"\nvar load = loader[int, string](w.NewLazySystemDLL)\nvar _ = load(\"ws2_32.dll\")\n",
		}, true, ""},
		{"cross-file LazyDLL type alias", map[string]string{
			"type.go":    "package probe\nimport w \"golang.org/x/sys/windows\"\ntype socketDLL = w.LazyDLL\n",
			"literal.go": "package probe\nvar _ = socketDLL{Name: \"ws2_32.dll\"}\n",
		}, true, ""},
		{"cross-file generic LazyDLL alias", map[string]string{
			"type.go":    "package probe\nimport w \"golang.org/x/sys/windows\"\ntype socketDLL[T any] = w.LazyDLL\n",
			"literal.go": "package probe\nvar _ = socketDLL[int]{Name: \"ws2_32.dll\"}\n",
		}, true, ""},
		{"cross-file multi-argument generic LazyDLL alias", map[string]string{
			"type.go":    "package probe\nimport w \"golang.org/x/sys/windows\"\ntype socketDLL[A, B any] = w.LazyDLL\n",
			"literal.go": "package probe\nvar _ = socketDLL[int, string]{Name: \"ws2_32.dll\"}\n",
		}, true, ""},
		{"cross-file converted Winsock constant", map[string]string{
			"name.go": "package probe\nconst winsock = string(\"ws2_32.dll\")\n",
			"load.go": "package probe\nimport w \"golang.org/x/sys/windows\"\nvar _ = w.NewLazySystemDLL(winsock)\n",
		}, true, ""},
		{"cross-file named string alias conversion", map[string]string{
			"type.go": "package probe\ntype dllName = string\n",
			"load.go": "package probe\nimport w \"golang.org/x/sys/windows\"\nvar _ = w.NewLazySystemDLL(dllName(\"ws2_32.dll\"))\n",
		}, true, ""},
		{"cross-file generic string alias conversion", map[string]string{
			"type.go": "package probe\ntype dllName[T any] = string\n",
			"load.go": "package probe\nimport w \"golang.org/x/sys/windows\"\nvar _ = w.NewLazySystemDLL(dllName[int](\"ws2_32.dll\"))\n",
		}, true, ""},
		{"scheme-relative endpoint", map[string]string{
			"probe.go": "package probe\nconst endpoint = \"//127." + "1:8080/\"\n",
		}, true, ""},
		{"shipped frontend", map[string]string{"frontend/app.js": "fetch(\"" + endpoint + "\")\n"}, true, ""},
		{"extensionless Winsock actual child", map[string]string{
			"probe.go": "package probe\nimport w \"golang.org/x/sys/windows\"\nvar _ = w.NewLazySystemDLL(\"ws2_32\")\n",
		}, true, ""},
		{"disqualified virtual host actual child", map[string]string{
			"probe.go": "package probe\nconst endpoint = \"https://mullion." + "localhost%3A8080/\"\n",
		}, true, ""},
		{"surplus-slash loopback actual child", map[string]string{
			"probe.go": "package probe\nconst endpoint = \"http:" + "////127.0.0.1:8080/\"\n",
		}, true, ""},
		{"mapped IPv6 loopback actual child", map[string]string{
			"probe.go": "package probe\nconst endpoint = \"http://[::ffff:127." + "0.0.1]:8080/\"\n",
		}, true, ""},
		{"parse failure", map[string]string{"broken.go": "package"}, true, ""},
		{"read failure", map[string]string{"unreadable.go": "package probe\n"}, true, "unreadable.go"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := writeNetworkFixture(t, testCase.files)
			executable, err := os.Executable()
			if err != nil {
				t.Fatalf("test executable: %v", err)
			}
			cmd := exec.Command(executable, "-test.run=^TestNoNetworkListener$", "-test.count=1")
			if testCase.readFailure != "" {
				cmd.Env = append(os.Environ(), networkGuardReadFailureEnv+"="+filepath.Join(root, filepath.FromSlash(testCase.readFailure)))
			}
			cmd.Dir = root
			hideChildConsole(cmd)
			out, err := cmd.CombinedOutput()
			failed := err != nil
			if failed != testCase.wantBad {
				t.Fatalf("guard failure = %v, want %v: %v\n%s", failed, testCase.wantBad, err, out)
			}
			if testCase.wantBad && strings.Contains(string(out), "PASS") {
				t.Fatalf("failing guard printed PASS:\n%s", out)
			}
		})
	}
}

func writeNetworkFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.invalid/networkguard\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}
