package host

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedSourcePlanCanonicalizesEveryConsumer(t *testing.T) {
	plan, err := buildSourcePlan(Config{VirtualHost: "App_One.Example"}.normalise())
	if err != nil {
		t.Fatal(err)
	}
	if !plan.embedded {
		t.Fatal("embedded source was not marked embedded")
	}
	if plan.origin.text != "https://app_one.example" ||
		plan.startURL != "https://app_one.example/index.html" ||
		plan.filterPattern != "https://app_one.example/*" ||
		plan.retryTarget != "https://app_one.example" ||
		plan.summary != "mullion: asset source=embedded-fs, virtual_host=https://app_one.example" {
		t.Fatalf("inconsistent plan: %+v", plan)
	}
	for _, source := range []string{
		"https://app_one.example/app",
		"https://APP_ONE.EXAMPLE/app",
		"https://app_one.example:443/app",
	} {
		if !plan.origin.matches(source) || !plan.messageSourceAllowed(source) || !plan.messageSourceTrusted(source) || plan.navigationOffOrigin(source, true) {
			t.Errorf("consumer disagreement for %q", source)
		}
	}
	for _, source := range []string{
		"https://app_one.example:444/app",
		"http://app_one.example/app",
		"https://evil.example/app",
	} {
		if plan.origin.matches(source) || plan.messageSourceAllowed(source) || plan.messageSourceTrusted(source) || !plan.navigationOffOrigin(source, true) {
			t.Errorf("consumer disagreement for rejected %q", source)
		}
	}
}

func TestSourcePlanConsumersRejectUnicodeCaseFoldConfusables(t *testing.T) {
	plan, err := buildSourcePlan(Config{VirtualHost: "Desktop.Kit"}.normalise())
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		"https://de\u017fktop.kit/app",
		"https://desktop.\u212ait/app",
	} {
		if plan.origin.matches(source) || plan.messageSourceAllowed(source) || plan.messageSourceTrusted(source) || !plan.navigationOffOrigin(source, true) {
			t.Errorf("consumer disagreement for rejected confusable %q", source)
		}
	}
}

func TestVirtualHostGrammarAcceptsUnderscoresAndIP(t *testing.T) {
	tests := []struct {
		raw    string
		host   string
		origin string
	}{
		{"MULLION.LOCALHOST", "mullion." + "local" + "host", "https://mullion." + "local" + "host"},
		{"app_one.internal", "app_one.internal", "https://app_one.internal"},
		{"_service._tcp.local", "_service._tcp.local", "https://_service._tcp.local"},
		{"_", "_", "https://_"},
		{"123.example", "123.example", "https://123.example"},
		{"127.0." + "0.1", "127.0." + "0.1", "https://127.0." + "0.1"},
		{"[0:0:0:0:0:0:0:1]", "::1", "https://[::1]"},
		{"::1", "::1", "https://[::1]"},
		{"[::ffff:192.0.2.1]", "::ffff:192.0.2.1", "https://[::ffff:192.0.2.1]"},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			host, err := canonicalVirtualHost(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			if host != test.host {
				t.Fatalf("host = %q, want %q", host, test.host)
			}
			plan, err := buildSourcePlan(Config{VirtualHost: test.raw}.normalise())
			if err != nil {
				t.Fatal(err)
			}
			if plan.origin.text != test.origin {
				t.Fatalf("origin = %q, want %q", plan.origin.text, test.origin)
			}
		})
	}
}

func TestVirtualHostGrammarRejectsAuthorityAndMalformedLabels(t *testing.T) {
	invalid := []string{
		"host:443",
		"user@host",
		"https://host",
		"host/path",
		"host?query",
		"host#fragment",
		`host\path`,
		" host",
		"host ",
		"host\nname",
		"host name",
		"host\tname",
		"host\rname",
		"host\x00name",
		"host\x7fname",
		"host%2ename",
		"host.",
		"one..two",
		"-host",
		"host-",
		"m\u00fcllion.local",
		"[fe80::1%25ethernet]",
		"[::1]:443",
		"[not-ipv6]",
		"127.1",
		"2130706433",
		"0177.0.0.1",
		"0x7f.0.0.1",
		"127.0.0.01",
	}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			if _, err := buildSourcePlan(Config{VirtualHost: raw}.normalise()); err == nil {
				t.Fatal("invalid virtual host accepted")
			}
		})
	}
}

func TestExternalURLIgnoresInvalidVirtualHost(t *testing.T) {
	plan, err := buildSourcePlan(Config{URL: testExternalURL, VirtualHost: "https://not-a-host"}.normalise())
	if err != nil {
		t.Fatal(err)
	}
	if plan.embedded || plan.origin.text != testExternalURL {
		t.Fatalf("external source plan = %+v", plan)
	}
}

func TestSourceOriginSummaryUsesCanonicalOriginOnly(t *testing.T) {
	for _, c := range []struct {
		raw, want string
	}{
		{"HTTPS://APP_ONE.EXAMPLE:443/private?token=secret", "https://app_one.example"},
		{"blob:https://evil.example/9f0c-uuid", "blob:https://evil.example"},
		{"filesystem:https://evil.example/temporary/x", "filesystem:https://evil.example"},
		{"not a URL", ":unknown"},
		{"blob:not-a-web-origin", ":unknown"},
		{"filesystem:", ":unknown"},
	} {
		if got := sourceOriginSummary(c.raw); got != c.want {
			t.Errorf("sourceOriginSummary(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestProductionConfigSourceProjectionIsConfinedToNormalizationAndSourcePlan(t *testing.T) {
	expected := map[string]int{
		"config.go:normalise:config.VirtualHost":            2,
		"source_plan.go:buildSourcePlan:config.URL":         2,
		"source_plan.go:buildSourcePlan:config.VirtualHost": 1,
	}
	observed := make(map[string]int)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		imports := make(map[string]bool)
		for _, imported := range file.Imports {
			name := filepath.Base(strings.Trim(imported.Path.Value, `"`))
			if imported.Name != nil {
				name = imported.Name.Name
			}
			imports[name] = true
		}
		owners := make(map[*ast.SelectorExpr]string)
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if selector, ok := node.(*ast.SelectorExpr); ok {
					owners[selector] = function.Name.Name
				}
				return true
			})
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "URL" && selector.Sel.Name != "VirtualHost") {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok {
				t.Errorf("%s independently projects Config.%s; use Host.source", fset.Position(selector.Pos()), selector.Sel.Name)
				return true
			}
			if imports[receiver.Name] {
				return true
			}
			function := owners[selector]
			if function == "" {
				function = "<package>"
			}
			key := name + ":" + function + ":" + receiver.Name + "." + selector.Sel.Name
			observed[key]++
			if _, ok := expected[key]; !ok {
				t.Errorf("%s independently projects Config.%s; use Host.source", fset.Position(selector.Pos()), selector.Sel.Name)
			}
			return true
		})
	}
	for projection, want := range expected {
		if got := observed[projection]; got != want {
			t.Errorf("%s selector count = %d, want %d", projection, got, want)
		}
	}
}
