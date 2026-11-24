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

## Run the Identity sandbox

With Go 1.25 or later, start a sandbox with a disposable user and project.

```sh
export SANDSTACK_USERNAME=fixture-user
export SANDSTACK_PASSWORD=fixture-password
export SANDSTACK_PROJECT=fixture-project
export SANDSTACK_ADMIN_TOKEN=fixture-admin-token
go run ./cmd/sandstack
```

It listens on `127.0.0.1:8080` by default. Use
`http://127.0.0.1:8080/identity/v3` as the Identity URL with domain `Default`
for the user and project. The
[Identity guide](docs/guides/identity.md) describes the authentication,
catalog, and token invalidation scenario.

See the [decision index](docs/decisions/README.md) for architecture and
[Contributing](CONTRIBUTING.md) for development guidance.
