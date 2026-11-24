# SandStack

SandStack is a project to build a lightweight OpenStack-compatible test
sandbox with programmable fault injection.

It is designed to run as an ephemeral dependency for automated tests of
services that integrate with OpenStack APIs. Its goals include
Keystone-compatible authentication, selected Nova, Neutron, Glance, and
Cinder APIs, deterministic state reset, and request tracing. Programmable
faults would let tests exercise backend failures, stale reads, timing
issues, and cross-service inconsistencies.

The [emulation decision](docs/decisions/0002-emulate-openstack-apis.md)
describes what the sandbox is designed to emulate.

## Status

SandStack is in the design stage. The [decision index](docs/decisions/README.md)
lists accepted and pending decisions. See [Contributing](CONTRIBUTING.md) for
code placement, documentation, and review guidance.
