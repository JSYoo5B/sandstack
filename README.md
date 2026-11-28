# SandStack

SandStack is a lightweight OpenStack test sandbox with programmable faults.
It runs as an ephemeral dependency for automated tests of OpenStack clients.
The fixture supports Keystone authentication and catalog discovery.
Nova covers server reads, power actions, locks, action history, console
output, metadata, flavor lookup, volume attachment, and key pairs. Glance covers image creation, upload, download, and deletion.
Lifecycle operations cover Neutron networks, IPv4 subnets, ports, and
logical routers, as well as Cinder volumes and snapshots.
Tests can alter fixture state, observe stale reads, and reset the sandbox
between cases.

The [emulation decision](docs/decisions/0002-emulate-openstack-apis.md)
describes what the sandbox is designed to emulate.

## Run the sandbox

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
for the user and project. The [Identity guide](docs/guides/identity.md)
covers token invalidation and reauthentication. The
[Compute guide](docs/guides/compute.md) covers Nova server operations and
links to the [fault controls](docs/guides/compute-faults.md),
[metadata guide](docs/guides/compute-metadata.md),
[key pair guide](docs/guides/compute-keypairs.md),
[action history guide](docs/guides/compute-actions.md), and
[console output guide](docs/guides/compute-console.md). The
[Networking guide](docs/guides/network.md) covers Neutron resources and
network downtime. The [Image guide](docs/guides/image.md) covers Glance
downloads and image deactivation. The [Block Storage guide](docs/guides/volume.md)
covers provisioning, metadata, and errors, with a separate
[snapshot guide](docs/guides/volume-snapshots.md).
[Inspect recent requests](docs/guides/requests.md) to verify what a client
attempted. [Reset the sandbox](docs/guides/reset.md) between test cases to
restore fixtures and invalidate issued tokens.

See the [decision index](docs/decisions/README.md) for architecture and
[Contributing](CONTRIBUTING.md) for development guidance.
