# ADR 0008: Implementation Scope

## Status

Accepted.

## Context

The OpenStack API surface is wider than any single downstream test needs.
SandStack needs enough real client behavior to make a failure observable and
enough control of its own state to make the same failure repeatable.

## Decision

- Implement one named downstream test scenario at a time. Identify the
  client, initial state, supported operations and versions, and observable
  result. Include the prerequisites the scenario exercises, such as
  authentication or catalog discovery.
- Implement only the behavior that serves the scenario. Advertise only
  implemented services in the catalog and document supported modes and
  material omissions beside the implementation.
- Build the normal and fault paths together when the scenario depends on
  fault injection. Use deterministic state changes with a baseline the same
  client can observe. Place HTTP test controls under `/_sandstack` with the
  dedicated administrator token from [ADR 0004](0004-service-url-layout.md).
- Start with disposable state and extend to persistence, runtime backends,
  or cross-service orchestration when a scenario needs them. Apply the
  evidence requirements in [ADR 0009](0009-verification-policy.md) to each
  advertised operation.

## Consequences

Service coverage grows with exercised scenarios. Each addition has a
bounded contract and may include faults as soon as its baseline can be
verified.
