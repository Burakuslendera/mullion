package host

import "testing"

// This file is exempt from the loopback-literal check in TestNoNetworkListener:
// it names loopback hosts only to prove Config.URL is pinned to the local
// machine. No socket is opened here.

const testExternalURL = "http://127.0.0.1:8080"

func TestExternalSourcePlanAcceptsOnlyLoopbackHTTP(t *testing.T) {
	valid := []string{
		"http://127.0.0.1:8080",
		"http://localhost:3000",
		"http://localhost",
		"https://127.0.0.1:8443",
		"http://[::1]:8080",
		"http://127.0.0.1:8080/app.html",
		"http://localhost:080/path",
		"https://localhost:0443/path",
	}
	for _, raw := range valid {
		plan, err := buildSourcePlan(Config{URL: raw}.normalise())
		if err != nil {
			t.Errorf("buildSourcePlan(%q) = %v, want nil", raw, err)
			continue
		}
		if plan.embedded || plan.filterPattern != "" {
			t.Errorf("buildSourcePlan(%q) produced embedded/filter state", raw)
		}
	}

	invalid := []string{
		"http://example.com",
		"https://192.168.1.10:8080",
		"http://10.0.0.5",
		"ftp://127.0.0.1",
		"file:///c:/app/index.html",
		"127.0.0.1:8080",
		"://nonsense",
		"http://localhost:",
		"http://localhost:notaport",
		"http://localhost:65536",
		"http://localhost:999999",
	}
	for _, raw := range invalid {
		if _, err := buildSourcePlan(Config{URL: raw}.normalise()); err == nil {
			t.Errorf("buildSourcePlan(%q) = nil error, want rejection", raw)
		}
	}
}

func TestExternalSourcePlanCanonicalizesOnceAndRedactsSummary(t *testing.T) {
	plan, err := buildSourcePlan(Config{
		URL:         "HTTP://alice:secret@LOCALHOST:80/private?token=secret#fragment",
		VirtualHost: "not a valid virtual host/ignored",
	}.normalise())
	if err != nil {
		t.Fatal(err)
	}
	if plan.origin.text != "http://localhost" || plan.retryTarget != "http://localhost" {
		t.Fatalf("origin/retry = %q/%q", plan.origin.text, plan.retryTarget)
	}
	if plan.summary != "mullion: asset source=external-url, url=http://localhost" {
		t.Fatalf("summary = %q", plan.summary)
	}
}

func TestExternalSourcePlanCanonicalizesNumericPortsForEveryConsumer(t *testing.T) {
	tests := []struct {
		raw        string
		startURL   string
		origin     string
		candidates []string
	}{
		{
			raw:        "http://localhost:080/private?token=secret#fragment",
			startURL:   "http://localhost/private?token=secret#fragment",
			origin:     "http://localhost",
			candidates: []string{"http://localhost/app", "http://localhost:80/app", "http://localhost:080/app"},
		},
		{
			raw:        "https://localhost:0443/private?token=secret#fragment",
			startURL:   "https://localhost/private?token=secret#fragment",
			origin:     "https://localhost",
			candidates: []string{"https://localhost/app", "https://localhost:443/app", "https://localhost:0443/app"},
		},
		{
			raw:        "http://127.0.0.1:08080/private?token=secret#fragment",
			startURL:   "http://127.0.0.1:8080/private?token=secret#fragment",
			origin:     "http://127.0.0.1:8080",
			candidates: []string{"http://127.0.0.1:8080/app", "http://127.0.0.1:08080/app"},
		},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			plan, err := buildSourcePlan(Config{URL: test.raw}.normalise())
			if err != nil {
				t.Fatal(err)
			}
			if plan.startURL != test.startURL || plan.origin.text != test.origin || plan.retryTarget != test.origin {
				t.Fatalf("canonical plan = start %q, origin %q, retry %q", plan.startURL, plan.origin.text, plan.retryTarget)
			}
			for _, candidate := range test.candidates {
				if !plan.origin.matches(candidate) ||
					!plan.messageSourceAllowed(candidate) ||
					!plan.messageSourceTrusted(candidate) ||
					plan.navigationOffOrigin(candidate, true) {
					t.Errorf("canonical port identity rejected %q", candidate)
				}
			}
		})
	}
}

func TestExternalStartURLIsNavigationOnlyCapability(t *testing.T) {
	plan, err := buildSourcePlan(Config{URL: "http://user:secret@localhost:080/private?query#fragment"}.normalise())
	if err != nil {
		t.Fatal(err)
	}
	if plan.startURL != "http://user:secret@localhost/private?query#fragment" {
		t.Fatalf("start URL = %q", plan.startURL)
	}
	if plan.navigationOffOrigin(plan.startURL, true) {
		t.Fatal("exact caller-authorized start URL was rejected by the navigation gate")
	}
	if plan.origin.matches(plan.startURL) ||
		plan.messageSourceAllowed(plan.startURL) ||
		plan.messageSourceTrusted(plan.startURL) {
		t.Fatal("navigation-only start capability was admitted as origin identity")
	}

	// The capability is deliberately byte-exact. WebView2 may report a
	// credential-free canonical form, which remains safe through origin.matches;
	// a partial credential or path rewrite must not inherit the caller's grant.
	for _, candidate := range []string{
		"http://user:different@localhost/private?query#fragment",
		"http://user:secret@localhost/other?query#fragment",
		"http://user:secret@localhost/private?other#fragment",
		"http://user:secret@localhost/private?query#other",
		"http://user@localhost/private?query#fragment",
		"http://@localhost/private?query#fragment",
		"http://other:secret@localhost/private?query#fragment",
		"http://user:secret@localhost:80/private?query#fragment",
		"HTTP://user:secret@LOCALHOST/private?query#fragment",
		"http://user:secret@localhost/priv%61te?query#fragment",
	} {
		if !plan.navigationOffOrigin(candidate, true) {
			t.Errorf("non-exact credentialed candidate %q passed the enabled gate", candidate)
		}
		if plan.navigationOffOrigin(candidate, false) {
			t.Errorf("disabled gate rejected %q", candidate)
		}
	}

	credentialFree := "http://localhost/other"
	if !plan.origin.matches(credentialFree) || plan.navigationOffOrigin(credentialFree, true) {
		t.Fatal("credential-free canonical origin candidate was rejected")
	}
}

func TestExternalSourcePlanOriginAdmission(t *testing.T) {
	plan, err := buildSourcePlan(Config{URL: testExternalURL}.normalise())
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		"http://127.0.0.1:8080/app",
		"HTTP://127.0.0.1:8080/app",
	} {
		if !plan.messageSourceAllowed(source) || !plan.messageSourceTrusted(source) {
			t.Errorf("trusted source %q rejected", source)
		}
	}
	for _, source := range []string{
		"http://127.0.0.1:9999/app",
		"http://127.0.0.1:65536/app",
		"http://127.0.0.1:notaport/app",
		"https://127.0.0.1:8080/app",
		"https://evil.example/app",
		"blob:http://127.0.0.1:8080/id",
		"http://127.0.0.1:8080@evil.example/app",
		"http://user:secret@127.0.0.1:8080/app",
	} {
		if plan.messageSourceAllowed(source) || plan.messageSourceTrusted(source) {
			t.Errorf("foreign source %q admitted", source)
		}
	}
	if plan.messageSourceAllowed("data:text/html,x") || plan.messageSourceTrusted("data:text/html,x") {
		t.Fatal("source-only policy must not grant fallback capability to an arbitrary data document")
	}
}

func TestSourcePlanNavigationGate(t *testing.T) {
	plan, err := buildSourcePlan(Config{URL: testExternalURL}.normalise())
	if err != nil {
		t.Fatal(err)
	}
	if plan.navigationOffOrigin("https://evil.example", false) {
		t.Fatal("disabled gate rejected a target")
	}
	for _, uri := range []string{"http://127.0.0.1:8080/app"} {
		if plan.navigationOffOrigin(uri, true) {
			t.Errorf("on-origin target %q rejected", uri)
		}
	}
	for _, uri := range []string{"http://127.0.0.1:9999/app", "about:blank", "blob:http://127.0.0.1:8080/id", "data:text/html,x"} {
		if !plan.navigationOffOrigin(uri, true) {
			t.Errorf("off-origin target %q admitted", uri)
		}
	}
}
