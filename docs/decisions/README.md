# Decisions

This directory holds the decisions carried into the current repository root.
Each record states one decision with its context and consequences. A decision
changes through a new record that supersedes the old one.

| ADR | Decision | Status |
|---|---|---|
| [0001](0001-router.md) | HTTP router: Chi | Accepted |
| [0002](0002-emulate-openstack-apis.md) | Emulate OpenStack APIs instead of deploying OpenStack | Accepted |
| [0003](0003-gophercloud-client-contract.md) | Gophercloud as the primary client contract | Accepted |
| [0004](0004-service-url-layout.md) | Single-port service URL layout | Accepted |
| [0005](0005-layered-architecture.md) | Layered architecture and dependency direction | Accepted |
| [0006](0006-runtime-determinism.md) | Injected clock and generators | Accepted |
| [0007](0007-dependency-policy.md) | Dependency policy | Accepted |
| [0008](0008-implementation-scope.md) | Implementation scope | Accepted |
| [0009](0009-verification-policy.md) | Verification policy | Accepted |

## Not Carried Over

The following earlier designs and artifacts were dropped on purpose. Bringing
them back requires a new decision record.

- SQLite as a required durable store target. Durable state is undecided.
- The phase ordering that required broad API compatibility before fault
  injection.
- The phased implementation plan and the MVP contract, including fixture
  credentials, Keystone request payloads, the catalog contents, and the request
  ID format. These described OpenStack wire behavior by hand and are revisited
  with the verification policy.
- All implementation code and tests.
