# ADR 0009: Verification Policy

## Status

Accepted.

## Context

Gophercloud demonstrates how a downstream client behaves against SandStack.
Its decoder accepts missing or unknown fields, so wire shape, errors, and
state transitions also need expectations derived independently of SandStack.

## Decision

- For each supported scenario, record the operations, the upstream source,
  and the behavior covered by checks. Use the OpenStack API reference,
  release-specific upstream code or tests, or a reference OpenStack run.
  Identify the release or source revision when behavior varies by version.
- Exercise advertised client flows with a pinned Gophercloud release
  against the assembled HTTP handler. Check the values and outcomes the
  client depends on, including relevant failures.
- Check externally visible protocol requirements with raw HTTP assertions
  for applicable statuses, headers, required body fields, or state
  transitions. Derive expectations from the upstream source. When upstream
  evidence is ambiguous, obtain a reference observation before claiming
  behavioral fidelity for the disputed case.
- Verify SandStack controls with deterministic before-and-after checks. Use
  a baseline through the same client path and assert which state changed and
  which requests observe it. Reference OpenStack need only cover the
  OpenStack behavior visible to the client.
- Identify what ran. Source-backed checks support a source-backed claim.
  For a reference run, record the OpenStack release, relevant configuration,
  and sanitized observations. Keep deployment-specific values out of shared
  fixtures while checking their meaningful relationships.

## Consequences

Client compatibility, source-backed wire behavior, and reference deployment
observations retain separate evidence. An operation with a clear upstream
contract can be implemented and checked before a reference deployment is
available.
