# Boot and delete servers in the Compute sandbox

[Compute sandbox](compute.md)

Use the project token from the [Identity fixture](identity.md) to boot a
server from the [Glance fixture image](image.md) with the
[fixture flavor](compute.md#read-the-server-flavor), wait until it is
`ACTIVE`, and delete it with Nova v2.1. SandStack creates or binds the
server's Neutron ports in the [Networking sandbox](network.md).

## Prepare a network

Nova attaches a server only to a network with a subnet. The
`fixture-network` starts without one, so create a subnet first, for
example with `subnets.Create` and CIDR `10.20.0.0/24`. A server can also
use a network that the client creates, or an existing port that has a
fixed IP. See [Neutron subnets](network-subnets.md) and
[Neutron ports](network-ports.md).

## Create a server

Use `servers.Create` in Gophercloud v2.15.0. Wrap the options in
`keypairs.CreateOptsExt` to set `key_name`. It sends `POST /servers`
with a `server` object. SandStack accepts these fields:

| Field | Rule |
| --- | --- |
| `name` | Required. The same rules as a [rename](compute.md#rename-a-server) |
| `imageRef` | Required. UUID of an `active` image owned by the project |
| `flavorRef` | Required. Flavor ID, integer, or a link whose last segment is the ID |
| `networks` | Optional. See [Choose networks](#choose-networks) |
| `security_groups` | Optional. Array of `{"name": ...}` objects with a group name or ID |
| `key_name` | Optional. Key pair of the token's user |
| `metadata` | Optional. Up to 128 items with the [metadata rules](compute-metadata.md) |
| `adminPass` | Optional. Any string |
| `OS-DCF:diskConfig` | Optional. `AUTO` or `MANUAL` |
| `user_data` | Optional. Base64 text up to 65535 characters, accepted and not exposed at v2.1 |
| `min_count`, `max_count` | Optional. Only `1` |

Unknown fields and the v2.1 schema violations return `400`. A missing or
empty `imageRef` returns `400` with `Missing imageRef attribute`. See the
[Nova create schema], [Nova create handler], and [Gophercloud create
options].

Nova v2.1 answers `202` with a `Location` header that holds the server's
`self` link. The `server` object contains `id`, `links`,
`OS-DCF:diskConfig`, `security_groups`, and `adminPass`. The
`security_groups` value echoes the request, or `[{"name":"default"}]`
when the request omits it. Without `adminPass`, SandStack generates a
12-character password from Nova's symbol groups, as Nova does with its
default `enable_instance_password` and `password_length`. See the [Nova
create view] and [Nova password generation].

The server belongs to the token's project and records the token's user
as `user_id`. Nova records a `create` action with the request ID. See
[Server action history](compute-actions.md).

### Rejected requests

SandStack checks a valid request in Nova's order and records no server or
port when it rejects one.

| Condition | Status |
| --- | --- |
| Unknown flavor | `400` |
| Unknown image | `400` |
| Unknown security group | `400` |
| Security group name that matches more than one group | `409` |
| Unknown port, or a port without a fixed IP | `400` |
| Port bound to a device | `409` |
| Fixed IP in use on the requested network | `400` |
| Unknown network, or a network without a subnet | `400` |
| Omitted networks with more than one available network | `409` |
| Unknown key pair | `400` |
| More than 128 metadata items | `403` |
| Image that is not `active`, including a [deactivated image](image.md) | `400` |

See the [Nova create errors], [Nova create checks], [Nova network
checks], and [Nova image status check].

## Choose networks

At v2.1 the `networks` array selects networks and ports. Gophercloud sends
it from `servers.CreateOpts.Networks` as a slice of `servers.Network`.

- `{"uuid": network_id}` creates a port on the network. Add `fixed_ip`
  to request an address. A network UUID that is not UUID-shaped, or an
  item without `uuid` or `port`, returns `400`.
- `{"port": port_id}` binds an existing unbound port. The port keeps its
  security groups. Repeating a port or combining it with `fixed_ip`
  returns `400`.
- An omitted or empty array selects the project's only network that is
  not external. Without such a network the server boots without ports.

Nova creates each new port with the server ID as `device_id`,
`compute:nova` as `device_owner`, and the requested security groups, or
the project's `default` group when none are requested. A bound port gets
the same `device_id` and `device_owner`. See the [Nova port creation] and
[Nova requested network checks].

Two cases pass Nova's API checks and fail later on the compute host:
selecting a network without a subnet by omitting `networks`, and
requesting an external network that belongs to another project. SandStack
accepts the request and reports the server in `ERROR` with a code `500`
fault whose message starts with `Build of instance {server_id} aborted:`
followed by Nova's exception message. No port remains. SandStack also
reports a port creation failure, such as an exhausted allocation pool,
this way. The fault wording follows Nova's build abort exception and has
not been checked against a reference deployment. See the [Nova build
network checks].

## Wait for the server

SandStack completes a build before it answers the create request. The
first `GET /servers/{server_id}` reports `ACTIVE`, so
`servers.WaitForStatus` returns on its first poll. Nova builds
asynchronously. See [Gophercloud WaitForStatus].

To observe `BUILD`, hold the builds of servers created afterwards with the
dedicated admin token. Then complete or fail each held build.

```sh
controls="http://127.0.0.1:8080/_sandstack/compute"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$controls/hold-builds"
# Create the server with the Compute API, then:
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" \
  "$controls/servers/${server_id}/complete-create"
```

While held, the server reports `BUILD` with `OS-EXT-STS:vm_state`
`building`, `OS-EXT-STS:task_state` `scheduling`, and
`OS-EXT-STS:power_state` `0`. Nova hides `addresses` while the server
builds, so they read `{}`. Its ports already exist in Neutron.
`complete-create` makes it `ACTIVE`. `fail-create` makes it `ERROR` with a
code `500` fault and Nova's scheduler message `No valid host was found.
There are not enough hosts available.` Each control returns `204`, also
when it repeats the outcome. Requesting the other outcome afterwards
returns `409`, and an unknown server returns `404`. Send `DELETE` to
`hold-builds` to complete later builds immediately again. See the [Nova
build state], [Nova address hiding], and [Nova scheduler failure].

## Read the server

Server detail and the detailed list show the server with these fields in
addition to the [fixture server fields](compute.md):

- `addresses` maps each network name to the port's fixed address and then
  its associated floating addresses. Each entry has `version`, `addr`,
  `OS-EXT-IPS:type` (`fixed` or `floating`), and
  `OS-EXT-IPS-MAC:mac_addr`.
- `security_groups` lists `{"name": ...}` for each group on each port.
  Nova omits it when the ports have no groups.
- `key_name` is the key pair name, or `null`.

SandStack reads the ports when it answers, so a floating IP associated in
Neutron appears immediately. Nova reads its cached network information,
which Neutron events refresh. At v2.1 the rename response omits
`key_name`, `security_groups`, and the address extension fields. Lists
order servers by creation time, newest first. See the [Nova server view],
[Nova address view], [Nova security group bindings], and [Nova update
view].

A created server supports the same actions, metadata, volume attachments,
and [fault controls](compute-faults.md) as the fixture server. Its
[console log](compute-console.md) starts empty.

## Delete a server

Use `servers.Delete`, which sends `DELETE /servers/{server_id}`. Nova
v2.1 returns `204` with an empty body in any server state, including
`BUILD` and `ERROR`. A locked server returns `409` and an unknown server
returns `404`. See the [Nova delete handler] and [Nova delete checks].

SandStack completes the deletion before it responds:

- Attached volumes become `available`.
- Bound ports that existed before the server lose their `device_id` and
  `device_owner` and keep their floating IPs.
- Ports created for the server are deleted, which disassociates their
  floating IPs.

Subsequent detail reads, actions, and deletes return `404`, and lists omit
the server. Nova shows actions of deleted servers only from microversion
2.21, so no `delete` record is observable at v2.1. See the [Nova network
deallocation].

[Reset the sandbox](reset.md) removes created servers, restores the
fixture server if it was deleted, and stops holding builds.

## Omissions

SandStack does not support block device mappings or boot from volume,
`personality`, `availability_zone`, `config_drive`, `accessIPv4`,
`accessIPv6`, `return_reservation_id`, scheduler hints, or creating more
than one server per request. These fields return `400`. Fields added
after v2.1, such as `description`, `tags`, or network `tag`, and the
`auto` and `none` network values also return `400`, as Nova v2.1 does.
Nova networks shared between projects, port quotas, and soft deletion with
`reclaim_instance_interval` are not modeled. Server detail does not include
`OS-EXT-AZ:availability_zone`, `config_drive`, `OS-SRV-USG` timestamps,
`OS-DCF:diskConfig`, `hostId`, or the access IP fields.

## Verification

Run `go test ./internal/api/server -run 'Boots|ServerCreateAndDelete|BuildControls' -count=1`
and `go test ./internal/app/compute -run TestServers -count=1`. The
Gophercloud tests boot servers on the default network, a network UUID,
and an existing port, wait with `servers.WaitForStatus`, and check detail,
lists, addresses with floating IPs, security groups, key name, metadata,
and the Neutron ports. They delete the servers and check the `404`, the
deleted and unbound ports, floating IP disassociation, and the detached
volume. Raw HTTP checks the `202` response fields and `Location`, the
`400`, `403`, and `409` cases, the locked delete, the `204` delete, and the
build controls before and after each outcome. The application tests check
the validation order, build failures, and port and volume release against
the pinned Nova source.

[Nova create schema]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L154-L230
[Nova create handler]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L700-L923
[Gophercloud create options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L441-L516
[Nova create view]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/views/servers.py#L78-L99
[Nova password generation]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/utils.py#L217-L250
[Nova create errors]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L835-L910
[Nova create checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L1140-L1162
[Nova network checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L2523-L2623
[Nova image status check]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L745-L746
[Nova port creation]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L1000-L1060
[Nova requested network checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L417-L478
[Nova build network checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L976-L1046
[Gophercloud WaitForStatus]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/util.go#L11-L24
[Nova build state]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2155-L2159
[Nova address hiding]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/views/servers.py#L609-L619
[Nova scheduler failure]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/scheduler/manager.py#L565-L572
[Nova server view]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/views/servers.py#L263-L345
[Nova address view]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/views/ips.py#L27-L53
[Nova security group bindings]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/security_group_api.py#L521-L563
[Nova update view]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1046-L1076
[Nova delete handler]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L925-L957
[Nova delete checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2818-L2824
[Nova network deallocation]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L1841-L1876
