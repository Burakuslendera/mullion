# 0030. The no-port guard exempts one virtual host name, not a file

**Status:** Accepted

## Context

The default virtual host was `mullion.local`, and every navigation to it waited
about two seconds before the first subresource was requested. A NetLog capture
named the span: a `HOST_RESOLVER_MANAGER_JOB` for `mullion.local:443` running
2.007 s. Seven runs measured 2.012 - 2.041 s document-to-first-subresource, and
45 consecutive in-origin navigations aborted without committing - issue #77 lived
inside that window. Moving the name under `.localhost` collapsed it: five runs on
2026-07-28, WebView2 runtime 150.0.4078.99, measured 47 - 141 ms
document-to-first-subresource, with `LaunchToWindowVisibleMs` falling from
2419 - 2543 to 448 - 630. The full measurement, the six negatives and what stays
open are in [assets.md](../assets.md); issues #85 and
#77 carry the captures.

Renaming the constant is a one-line change that fails six tests. Five of them pin
the current default and are ordinary updates. The sixth is `TestNoNetworkListener`,
which enforces [0002](./0002-no-local-port.md)'s promise that no local port is
ever opened. It greps every `.go` file but its own for `net.Listen`,
`http.ListenAndServe`, `http.Serve(`, `httptest`, `127.0.0.1` and `localhost`.
[0012](./0012-config-url-loopback.md)
narrowed the last two to `loopback.go`/`loopback_test.go`, the files that exist to
*reject* a non-loopback `Config.URL`.

That guard reads the `localhost` inside `mullion.localhost` as a loopback literal.
It is a substring match, and it is right to be: the tier exists so that no file
can quietly hard-code an address on the local machine. But the name it now
catches is not an address. The runtime resolves the name like any other - that is
the whole reason it has to sit under this TLD - but nothing *connects* to it:
`WebResourceRequested` answers the request in this process, and 0002's guarantee
is untouched. The guard cannot tell a name from an address, and that distinction,
narrower than "the name appears in the file", is this record's content.

## Decision

The loopback tier gains **one exemption, and it is a name rather than a file.**
The scan removes occurrences of the exact token `mullion.localhost` from a file's
source before matching, **but only where the name stands alone.** Everything else
is unchanged: listener/DLL markers have no file exception; full endpoint-only
authority is limited to the loopback and source-plan fixture files; the three
other fixture locations admit only their exact token; same basenames get nothing.

Standing alone means no label character in front of it, and nothing behind it
that continues a name or turns one into an address: another label, an FQDN's
trailing dot, a port, userinfo, a percent escape. The reason is not syntactic
tidiness. The request filter is registered for this origin exactly -
`AddWebResourceRequestedFilter(origin()+"/*")` - so `mullion.localhost` is a name
this process answers, while `preview.mullion.localhost` does not match that
pattern and raises no `WebResourceRequested` at all. That much is in the code.
What follows is inference, not measured here: an unintercepted request goes to
the network stack, and RFC 6761 reserves the whole `.localhost` subtree for the
loopback interface, so the graft is a local address wearing the product's name -
which is precisely what this tier exists to catch. No navigation to a subdomain
of the virtual host has ever been run against this repository.

The token is spelled out in the test rather than derived from
`defaultVirtualHost`, and the test fails if the two stop matching. Deriving it
would exempt whatever the default became - including `localhost` itself - which
would turn the guard into a mirror of the code it checks.

## Alternatives rejected

**Add `config.go` to the file exemption list.** One line, and it follows the
precedent 0012 set. But the file exemption carries a stated reason - these files
name loopback hosts *in order to reject them* - and that reason is not true of
`config.go`. It would also legalise `127.0.0.1` in the file that defines the
configuration surface, which is a wider hole than the one being closed, and it
grows the exemption in the unit that hides the most.

**Match on a word boundary: allow `localhost` when a dot precedes it.** This is
the principled-looking option and it is the dangerous one. RFC 6761 pins the
whole `.localhost` subtree to loopback, so `app.localhost:8080` is a genuine
loopback address - the rule would legalise precisely what the tier exists to
catch. It is the inverse of the rule adopted above, which disqualifies an
occurrence *because* a label precedes it.

**A port as the only discriminator.** The first version of this exemption removed
the name unless a `:` followed it, on the reasoning that a bare name is a name and
a name with a port is an address. It was written, and an adversarial pass
refuted it before it shipped: `https://preview.mullion.localhost/` and
`https://mullion.localhost./index.html` both passed the guard, and both are real
loopback addresses for the reason given in the Decision. The premise was wrong -
a bare name is not necessarily one this process answers - so the rule now tests
both sides of the token rather than one character on the right. The port case is
still caught; it is no longer the only thing caught.

**Split the literal in the source.** The split has to cut the needle itself -
`"mullion." + "localhost"` still fails, because the second half is the banned
token whole; it takes `"local" + "host"` to hide. `leak_test.go` builds its own
needles that way so the scanner does not match itself, which is legitimate for a
scanner; the same trick in production code inverts the meaning, because the guard
would then be satisfied by any file willing to concatenate.

**Drop the loopback tier and keep the listener markers.** The listeners are the
socket, so 0002's promise would still be guarded. But 0012 considered removing
this tier and narrowed it instead: a hard-coded loopback URL is the shape the
promise decays into, and it is caught by nothing else.

## Consequences

**A future rename is a two-file change, deliberately.** Any virtual host name
that is not exactly `mullion.localhost` fails the guard until this exemption is
edited. That cost is the point: the default carries a measured 2 s of behaviour,
and it should not move without someone reading why.

**The guard now knows one product string.** A test that scans the tree is coupled
to a constant it scans for. The pin makes the coupling loud rather than quiet: if
`defaultVirtualHost` changes, the failure names this record.

**Go comments are outside this guard.** The AST pass deliberately does not parse
comments, so policy prose can name the reserved TLD without becoming a finding.
String literals and imported API selectors remain inspected regardless of build
tags. This scope is explicit: the guard proves selected syntax, not arbitrary Go
prose or semantic whole-program behaviour.

**Endpoint fixture authority is explicit and token-specific.** Full endpoint-only
authority belongs to `host/loopback.go`, `host/loopback_test.go`,
`host/source_plan_test.go` and `host/source_plan_windows_test.go`. The unsupported
architecture file admits one exact split legacy token; errorpage/system-browser
files admit only their bracketed token, never another endpoint in the same literal.
No path exempts API/DLL findings; same basenames elsewhere have no authority.

**The exemption is narrower than the thing it protects.** `Config.VirtualHost` is
a caller's field, and nothing checks that a caller's name is under `.localhost`.
A caller passing `app.local` gets the two-second wait back, silently, and the
guard has nothing to say about it - it scans this repository's source, not a
caller's value. That is documented on the field in `config.go`, and it is a real
gap rather than an oversight.

## What would change our mind

- **Upstream fixes the resolution.** [WebView2Feedback #2381](https://github.com/MicrosoftEdge/WebView2Feedback/issues/2381)
  is open since 2022, tagged priority-low. If the runtime stops resolving a name
  it answers in process, `.localhost` is no longer worth anything and both the
  default and this exemption can go back.
- **A second name needs exempting.** One string is an exemption; a list is a
  policy in disguise. The day a second name is added, the answer is a rule that
  classifies by shape - name versus address - not a longer list.
- **A loopback URL survives review.** One already did during this change, in the
  rule's first version. If another reaches `main` past the boundary test, the
  answer is not a third text rule: it is that a source scan has reached its
  ceiling and the invariant needs checking at run time instead.

## Evidence

- `host/network_guard_test.go`: module traversal, import-aware API identity,
  lexical-shadow rejection and fail-closed errors.
- `host/network_dll_guard_test.go`: supported loaders, assigned/parenthesized
  targets, generic string/function/`LazyDLL` aliases, local/cross-file types and
  explicit/extensionless module names.
- `host/network_endpoint_guard_test.go`: candidate-relative scheme/relative/
  standalone placement, browser legacy IPv4/root dots, special-scheme separator
  runs, mapped wildcard IPv6, encoded external labels and token-specific authority.
- `host/network_guard_policy_test.go`: shadows, generic/assigned DLL bindings,
  virtual-host disqualifiers, clean controls and actual-child modules for the
  named source forms, selected failures and verdict wiring.
- The source guard excludes comments and cannot see names assembled only at run
  time. `docs/guard-verification.md` records those syntactic proof ceilings.
- `host/config.go`: `defaultVirtualHost`, and the field comment that carries the
  measurement and the caller-side gap.
- `docs/assets.md`: the capture, the table, the six negatives, the
  readings the live run replaced, and the two measurement traps it exposed - the
  profile warmth and the end of the window.
- Issues #85 (the wait) and #77 (the aborts it caused); both close with the
  rename this record unblocks.

> Last updated: 2026-08-10 | Editor: OpenAI (GPT-5.6) | Change: reconcile exact endpoint authority and lock assigned/generic string and DLL loaders, browser surplus separators, mapped wildcard IPv6, encoded external labels, virtual-host disqualifiers, actual-entrypoint failures and syntactic ceilings.