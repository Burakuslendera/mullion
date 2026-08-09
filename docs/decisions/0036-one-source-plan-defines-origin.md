# 0036. One source plan defines the frontend origin

**Status:** Accepted

## Context

`Config.VirtualHost` and `Config.URL` feed the initial navigation, embedded-asset
filter, bridge admission, navigation gate, fallback Retry target and source log.
Parsing or normalising them independently lets those consumers disagree about an
origin. Browser host parsing makes the disagreement security-relevant: legacy
numeric IPv4 forms, default ports, userinfo and IPv6 spelling do not preserve
textual identity.

## Decision

`New` builds one immutable source plan before native setup. Embedded mode accepts
lowercaseable ASCII labels (`a-z`, digits, `-`, `_`, `.`), retaining underscore
compatibility; it accepts only IPv4/IPv6 literals parsed by `netip`. It rejects
Unicode, trailing dots, empty or malformed labels, authority/scheme/path syntax,
percent escapes, zones and browser legacy numeric IPv4 forms. The plan emits one
canonical HTTPS origin, start URL, request-filter pattern, Retry origin and
redacted log summary.

`Config.URL` is parsed once as an absolute HTTP(S) loopback URL. Its canonical
origin and preserved caller path/query/fragment supply the same consumers. The
exact emitted start URL is a navigation-only capability, including when it
retains caller userinfo; no other userinfo-bearing candidate proves origin
identity. Filter registration, asset checks, bridge trust, later navigation
policy, Retry and logs consume the plan, not the configuration fields. On
supported Windows, an invalid plan stops before DPI, runtime discovery, COM,
class or HWND work. On unsupported Windows, `ErrUnsupportedArchitecture` remains
the first `Run` preflight result.

## Alternatives rejected

**Let `net/url` or WebView2 define `VirtualHost`.** `VirtualHost` is a host token,
not an authority or URL. General URL parsing admits syntax this API cannot use,
and browser parsing accepts legacy numeric forms that `netip` does not identify
as the same address.

**Require DNS hostnames and remove underscores.** That is simpler grammar but
breaks accepted existing configurations without improving origin agreement.
Underscores remain an explicit compatibility cost.

**Normalise at every use.** Repetition appears defensive but creates several
parsers whose drift is the defect this decision prevents.

## Consequences

The accepted grammar is narrower than browser host syntax: Unicode names,
trailing-dot FQDNs, authority strings and legacy numeric IPv4 spellings now fail
preflight. Existing underscore hosts keep working. Every new origin-sensitive
consumer must take the source plan; reading `Config.URL` or `Config.VirtualHost`
directly in production is a regression. `Config.URL` may retain userinfo only in
the exact initial navigation capability; userinfo never proves origin identity.

## What would change our mind

Supersede this record if WebView2 exposes one typed, immutable origin identity
that covers filter registration, event-source comparison and navigation targets
without reparsing, or if compatibility evidence requires a currently rejected
host form and one canonical representation can be proved across every consumer.
A new production read of either raw source field outside normalisation and plan
construction is the trip-wire that the implementation has already drifted.

## Evidence

- `host/source_plan.go` and `host/loopback.go`: grammar, canonical origin and the
  only production parser of `Config.URL`.
- `host/source_plan_test.go`: accepted/rejected host matrix, consumer agreement
  and the AST guard confining raw configuration reads.
- `host/loopback_test.go` and `host/webview_event_observations_windows_test.go`:
  one-pass external URL/port canonicalisation, exact credentialed startup
  capability, redacted summaries and userinfo-bearing identity rejection.
- `host/source_plan_windows_test.go` and
  `host/architecture_gate_unsupported_windows_test.go`: supported and
  unsupported preflight ordering.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: record one canonical origin plus the exact credentialed startup capability and VirtualHost compatibility boundary.
