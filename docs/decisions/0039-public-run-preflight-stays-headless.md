# 0039. Public Run preflight stays headless

**Status:** Accepted; supersedes [0006](./0006-tests-stay-headless.md)

## Context

The original headless rule prohibited every test call to `Run()`. That kept tests
away from windows and message pumps, but it also excluded the public boundary
where source-plan and architecture preflight errors are ordered and returned.
Calling a private helper cannot prove that `Run` preserves that ordering.

A public call is still headless when a deterministic seam or build path makes it
return before runtime discovery, COM, window-class or `HWND` creation, every
Win32 entry point, and message pumping. The proof must observe the forbidden
seams, not merely choose an input that is expected to stop early.

## Decision

Tests remain headless. A test may call public `Run` only when a deterministic
seam or build path proves a pre-native return and the test asserts that runtime
discovery, COM, class/`HWND` creation, Win32 calls, and message pumping were not
reached. Every other `Run` call, and every test that creates a window or needs a
display or WebView2 runtime, remains prohibited.

This protected-rule refinement has explicit Tier-3 human approval.

## Alternatives rejected

**Keep the blanket public `Run` ban.** This is simple to review, but it weakens
coverage of public preflight precedence: private helper tests do not prove that
the exported method calls them before native startup.

**Treat an early-return input as sufficient.** A refactor can move runtime or
Win32 work ahead of the check while the selected input still eventually returns
the expected error. An input is not a boundary; observable forbidden seams are.

**Permit general `Run` tests behind mocks.** A mocked COM or window layer would
exercise a second implementation of the native lifecycle and could conceal the
ABI and shell behaviour the headless rule exists to avoid.

## Consequences

Public-boundary preflight precedence can be locked without weakening the CI
contract. Such tests carry a higher burden than ordinary pure-function tests:
they must fail if any forbidden seam is reached, and they must remain valid on a
headless worker with no runtime installed.

Live window, rendering, input, frame, DPI, and message-loop behaviour remains in
the manual acceptance checklist. A deterministic preflight `Run` test is not
evidence for any of those behaviours.

## What would change our mind

Supersede this record if public preflight is removed from `Run`, or if a reliable
window-driving environment can exercise WebView2 input and native lifecycle in
CI without a desktop-dependent result. A preflight test reaching an unobserved
native side effect is the trip-wire for tightening or removing the exception.

## Evidence

- Explicit Tier-3 human approval on 2026-08-06 refined the protected rule to this
  deterministic pre-native exception.
- `host/source_plan_windows_test.go` calls `Run` through a discovery seam and
  checks that invalid source plans do not reach discovery.
- `host/architecture_gate_unsupported_windows_test.go` checks the public
  unsupported-architecture return and its forbidden native-startup state.
- `host/host_other_test.go` exercises the build-selected non-Windows return,
  where no Windows implementation is present.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: supersede the blanket Run-test ban with a human-approved deterministic pre-native exception that proves forbidden seams remain untouched.
