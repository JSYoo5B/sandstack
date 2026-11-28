# ADR 0011: Upstream Baseline

## Status

Accepted. Refines the release identification in
[ADR 0009](0009-verification-policy.md).

## Context

ADR 0009 requires identifying the release or source revision when behavior
varies by version, but it does not say which release to follow. Scenarios
were pinned to unrelated releases, some years apart, and some links point at
moving branches. An old pin can make a current client flow unimplementable
when the feature only exists in later releases.

## Decision

- Derive the expected behavior of new work from the latest upstream source at
  the time of the work: the newest commit on the default branch of each
  OpenStack project the scenario depends on, or the newest release when the
  scenario depends on a released microversion or API version.
- Pin that source to an immutable commit or release tag in guides and test
  links. Do not cite moving branches such as `master` as evidence.
- Different scenarios may carry different baselines. Each guide records the
  baseline it was verified against.
- When work changes an existing scenario, re-derive the affected behavior from
  the latest upstream source and update the pins it touches. Leave untouched
  scenarios on their recorded baselines.
- Advertise API versions and microversions as implemented, not as the newest
  value the baseline supports.

## Consequences

- Features that exist only in recent releases can be implemented without a
  separate baseline decision.
- Guides may cite several revisions of one project until older scenarios are
  revisited.
- Existing `master` links are corrected when their scenarios are next changed.
