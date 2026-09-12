# 0051. A private command's Run token comes from the crypto source, not a counter

**Status:** Accepted

## Context

The private window commands (`WM_APP+21` through `WM_APP+29`, decision 0004)
are the only path that mutates the window lifecycle: the receiver
(`dispatchNativeHostCommand`) applies a command only when the callback HWND and
the `lParam` token still name the active Run. Those message numbers are fixed
Win32 constants, so any same-desktop process can construct the messages; the
token is the only value in them a receiver-side check can hold against a peer.

The token was allocated by a process-global counter starting at `1`, retained
for the life of the session and released at teardown. The retention and
process scope already closed the stale-generation hole — Windows recycles
HWNDs, and an older Run's delayed command had to be kept off a recycled handle
— but nothing about the value was secret. A counter that starts at `1` hands
every same-desktop process the value the first session will carry, and the
next ones after it.

Issue #141 measured that gap on Windows 11 (build 26200.9168, amd64, standard
user): a helper process at the same user and integrity level reached the
private quit with one prearranged candidate, the target exited, and `OnClose`
was not invoked a second time. `WM_CLOSE` honors `Config.OnClose`; the tagged
private quit destroys the window directly, so a peer that can guess the token
does not need to pass the application's close policy at all.

## Decision

Each reservation draws one candidate from `crypto/rand` through
`nativeRunTokenSource`. A candidate that is zero or already live is discarded
and redrawn; the live set is retained exactly as before. A failed source fails
closed: `reserve` returns the error, `beginRun` refuses to start the session,
and nothing is reserved. The receiver's token-and-HWND comparison is
unchanged.

## Alternatives rejected

**Verify the sender's identity at the receiver.** `GetWindowThreadProcessId`
on the sending process would let the receiver admit only its own process.
Rejected: a posted message carries no sender identity the receiver can read —
by the time the message dispatches, it runs on the receiver's own thread, and
the queued envelope has no attribution. The check is not implementable for the
`PostMessage` delivery these commands use.

**Seed a counter with one random draw per process.** A per-process random
base plus increments keeps the counter machinery and spends one RNG call per
process instead of per session. Rejected: it narrows the whole secrecy budget
to a single draw and makes the values around any observed token trivially
extrapolable; a per-session draw costs the same call and keeps every session
independent.

**Randomise the message identifiers.** `RegisterWindowMessage` with per-process
strings would stop a peer from constructing the messages by number. Rejected:
a same-desktop peer can register the same strings and learn the identifiers,
so this is obscurity of the address, not of the authority; the token remains
the capability and still needs to be unguessable.

**Treat an equal-integrity peer as out of scope.** UIPI separates integrity
levels, not same-level peers, and this boundary already assumes the same
desktop is hostile: the stale-generation and recycled-HWND guards exist
precisely because messages can arrive from anywhere at the receiver's level.
Rejected: exempting the quit command from that assumption would leave the
one lifecycle action that bypasses `OnClose` authorized by a guessable value.

## Consequences

On `386` the token is 32 bits, and brute force at message rate is expensive
but not out of reach; the supported runtime is amd64
(decision 0034), where `2^64` candidates are. The collision guard's job
changes from counter wrap to candidate collision, and the live set must keep
its retention discipline — a discarded candidate is redrawn, never reused
while live.

A Run whose token cannot be sourced cannot start, and the error surfaces
through `Run`'s existing error path. There is no fallback allocator; any
fallback reintroduces the predictable identity this record exists to remove.

The token is now a high-entropy secret by construction. It must never be
logged, forwarded across a process boundary, or embedded in a diagnostic;
nothing in the log sink formats it today, and code that touches
`activeRunToken` must keep it that way. Tests stub
`nativeRunTokenSource` to pin the discard and failure paths deterministically;
the stub is authority-adjacent, so the restore discipline is load-bearing.

## What would change our mind

- A Windows mechanism that attributes a posted message to its sending process
  at the receiver would make sender verification cheaper and stronger than a
  bearer capability; the token could then be demoted to session bookkeeping.
- Any feature that carries the token across a process or log boundary destroys
  the secrecy assumption; such a feature must re-key the capability first, not
  disclose this one.
- A measured same-integrity attack that predicts or observes issued tokens
  through a channel cheaper than brute force would mean `lParam` itself is the
  wrong channel for authority.

## Evidence

- [Issue #141](https://github.com/Burakuslendera/mullion/issues/141) (P0
  blocker) reports the bounded Win11 observation: one prearranged
  token/window pair reached the private lifecycle action, while zero, stale
  and wrong-window candidates were denied.
- `TestBeginRunSourcesItsRunTokenFromTheCryptoSource` pins the issued identity
  to the sourced candidate with one draw per reservation;
  `TestRunTokenSourceDrawsDistinctNonZeroValues` pins the production source to
  distinct non-zero draws.
- `TestNativeRunTokenRegistrySkipsZeroAndLiveCandidates` and
  `TestNativeRunTokenRegistryFailsClosedWhenSourceFails` pin the discard and
  fail-closed paths; `TestBeginRunFailsClosedWhenRunTokenSourceFails` pins that
  a failed source starts no session and leaks no reservation.
- `TestPredictedCandidatesCannotAuthorizePrivateCommand` pins the receiver
  contract: prearranged candidates apply nothing, the owner's token applies
  once.

> Last updated: 2026-09-12 | Editor: ZCode (GLM-5.3-Flash) | Change: create the record — a private command's Run token is drawn from the crypto source per session, not counted (issue #141).
