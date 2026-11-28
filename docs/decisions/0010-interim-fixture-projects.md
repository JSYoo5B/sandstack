# ADR 0010: Interim Fixture Projects

## Status

Accepted. Interim until superseded as described below.

## Context

Keystone exists to isolate projects. Scenarios that cross a project boundary
cannot be verified with a single fixture project: transferring a volume,
sharing an image with a member project, attaching another project's port, or
being refused a change to another project's resource. Scenarios that would
create or manage projects through the Identity API are not implemented, and
they cannot be verified before the identity behavior they depend on is.

## Decision

- Define fixture users and projects through environment configuration at
  startup, as an interim measure. The existing fixture user and project remain
  the primary fixture. Additional fixtures are optional, each one user with
  the member role on its own project in the `Default` domain.
- Scope every token to the project it authenticated against. Services record
  the owning project on each resource and decide visibility and ownership from
  the token's project, not from a single seeded project.
- Keep configured fixtures limited to what scenarios need. Do not add domains,
  extra roles, role assignments, or project hierarchy through configuration.
- Replace configured fixtures with one of the following, through a new record
  that supersedes this one:
  - Identity API operations that create projects, users, and role
    assignments, implemented as scenarios under
    [ADR 0008](0008-implementation-scope.md) and verified under
    [ADR 0009](0009-verification-policy.md).
  - SandStack administration controls under `/_sandstack` that create and
    remove fixture identities with the administrator token.

## Consequences

- Cross-project scenarios become testable with tokens from separate fixtures.
- Services that compare against one seeded project change to per-resource
  ownership.
- Environment fixture configuration is a temporary contract. Its removal is
  expected and is not a compatibility break once a replacement exists.
- The configuration stays environment-based within
  [ADR 0007](0007-dependency-policy.md).
