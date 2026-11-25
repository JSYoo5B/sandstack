# SandStack

SandStack is a lightweight OpenStack test sandbox with programmable faults.
It runs as an ephemeral dependency for automated tests of OpenStack clients.
The current fixture supports Keystone authentication and catalog discovery,
along with selected Nova, Neutron, Glance, and Cinder reads. Tests can
create and delete Neutron networks, IPv4 subnets, and ports, alter fixture state,
observe stale reads, and reset the sandbox between cases.

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
[Compute guide](docs/guides/compute.md) covers Nova server faults and stale
reads. The [Networking guide](docs/guides/network.md) covers Neutron network
reads and operational downtime. The [Image guide](docs/guides/image.md)
covers Glance metadata and image deactivation. The
[Block Storage guide](docs/guides/volume.md) covers Cinder volume errors.
[Inspect recent requests](docs/guides/requests.md) to verify what a client
attempted. [Reset the sandbox](docs/guides/reset.md) between test cases to
restore fixtures and invalidate issued tokens.

See the [decision index](docs/decisions/README.md) for architecture and
[Contributing](CONTRIBUTING.md) for development guidance.
