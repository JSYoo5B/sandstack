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
| `imageRef` | Required unless `block_device_mapping_v2` is set. UUID of an `active` image owned by the project |
| `flavorRef` | Required. Flavor ID, integer, or a link whose last segment is the ID |
| `networks` | Optional. See [Choose networks](#choose-networks) |
| `security_groups` | Optional. Array of `{"name": ...}` objects with a group name or ID |
| `key_name` | Optional. Key pair of the token's user |
| `metadata` | Optional. Up to 128 items with the [metadata rules](compute-metadata.md) |
| `adminPass` | Optional. Any string |
| `OS-DCF:diskConfig` | Optional. `AUTO` or `MANUAL` |
| `user_data` | Optional. Base64 text up to 65535 characters, accepted and not exposed at v2.1 |
| `min_count`, `max_count` | Optional. Only `1` |
| `block_device_mapping_v2` | Optional. See [Boot from a volume](#boot-from-a-volume) |

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

## Boot from a volume

Leave `imageRef` empty and set `servers.CreateOpts.BlockDevice` in
Gophercloud v2.15.0. It sends `block_device_mapping_v2` with one item per
`servers.BlockDevice`, and always sends `boot_index` and
`delete_on_termination`. SandStack supports one item with
`destination_type` `volume` and `boot_index` `0`:

- `source_type` `volume` with `uuid` set to a bootable Cinder volume of
  the project. See [Volume sources](volume-sources.md) for creating one
  from an image.
- `source_type` `image` with `uuid` set to a Glance image and
  `volume_size` in GiB. Nova creates the volume when the build completes.

`delete_on_termination` is optional and defaults to `false`. The item
also accepts `boot_index` and `volume_size` as strings and
`delete_on_termination` as one of the v2.1 boolean strings, as the [Nova
block device schema] does. Other v2.1 item fields, more than one item, an
item together with `imageRef`, and other source or destination types
return `400`. An empty array is ignored, so `imageRef` is then required.
See the [Nova block device processing] and [Gophercloud block device
options].

### Rejected block device requests

Nova rejects these requests with `400`. SandStack checks them in Nova's
order and records no server and no volume change:

| Condition | Message |
| --- | --- |
| Missing `uuid` | `Block Device Mapping is Invalid: Missing device UUID.` |
| `uuid` with `source_type` `blank` | `Block Device Mapping is Invalid: Invalid device UUID.` |
| `source_type` `image` with `destination_type` `local` | `Block Device Mapping is Invalid: Mapping image to local is not supported.` |
| Unknown boot volume | `Block Device Mapping is Invalid: failed to get volume {id}.` |
| Boot volume that is not bootable | `Block Device {id} is not bootable.` |
| Unknown boot image | `Block Device Mapping is Invalid: failed to get image {id}.` |
| Boot image that is not `active` | `Image None is not active.` |
| `volume_size` below the image `min_disk` | `Volume is smaller than the minimum size specified in image metadata. ...` |
| Unknown volume outside the boot sequence | `Volume {id} could not be found.` |
| No device with `boot_index` `0` | `Block Device Mapping is Invalid: Boot sequence for the instance and image/block device mapping combination is not valid.` |
| Image source without `volume_size` | `Images with destination_type 'volume' need to have a non-zero size specified` |
| Volume that is not `available` | `Invalid volume: Invalid input received: Invalid volume: Volume {id} status must be available or downloading to reserve, but the current status is {status}. (HTTP 400)` |

The boot volume and image lookups run after the flavor check and before
the security group, network, key pair, and metadata checks. The image
status and minimum disk checks follow the metadata quota, and the boot
sequence and volume state checks come last. Nova reports `None` as the
image ID because a volume-backed request has no `imageRef`. Nova checks
the image `min_disk` against `volume_size` and does not check the flavor
disk for a volume root disk. The last message wraps Cinder's attachment
error. A real deployment appends Cinder's request ID, which SandStack
omits. See the [Nova volume boot metadata], [Nova boot image checks],
[Nova block device validation], and [Cinder attachment reservation].

### Root volume

Nova reserves an existing volume when it accepts the request. Cinder
then reports it as `reserved` and lists no attachments. When the build
completes, SandStack attaches the volume as `/dev/vda` and Cinder reports
it `in-use` with the attachment. For an image source, SandStack creates a
volume with an empty name and description in the default volume type,
provisions it as [Cinder does for an image source](volume-sources.md), and
attaches it the same way. The new volume is bootable and exposes the
image metadata. See the [Nova volume creation] and [Cinder attachment
view].

A volume-backed server shows `"image": ""` at v2.1, which Gophercloud
reads as a nil `Server.Image`. The root volume appears in
`os-extended-volumes:volumes_attached` and `volumeattach.List`, and more
volumes attach from `/dev/vdb`. Detaching the root volume returns `400`
with `Cannot detach a root device volume` after the volume and attachment
lookups. See the [Nova server image view] and [Nova root detach check].

Deleting the server detaches the root volume. With
`delete_on_termination` set, SandStack then deletes the volume, unless
Cinder refuses, for example because the volume has snapshots. Otherwise
the volume becomes `available`. See the [Nova BDM cleanup].

With [held builds](#wait-for-the-server), an existing volume stays
`reserved` and an image source creates no volume until `complete-create`.
`fail-create` models Nova's scheduling failure. The server keeps its
reserved volume until it is deleted, and an image source creates no
volume. See the [Nova cell0 block devices]. A build that fails on the
compute host, such as a [network failure](#choose-networks), detaches
the reserved volume and deletes it when `delete_on_termination` is set.
If Cinder rejects the image source volume when the build completes, for
example because the image was deactivated, the server reports `ERROR`
with the fault `Build of instance {server_id} aborted: Failure prepping
block device.` and releases its ports. See the [Nova build cleanup] and
[Nova block device preparation failure].

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

SandStack does not support the legacy `block_device_mapping`,
`personality`, `availability_zone`, `config_drive`, `accessIPv4`,
`accessIPv6`, `return_reservation_id`, scheduler hints, or creating more
than one server per request. These fields return `400`. Fields added
after v2.1, such as `description`, `tags`, or network `tag`, and the
`auto` and `none` network values also return `400`, as Nova v2.1 does.
Nova networks shared between projects, port quotas, and soft deletion with
`reclaim_instance_interval` are not modeled. Block device mappings for
snapshots, blank volumes, swap, ephemeral disks, extra volumes, and
`device_name` are not supported. A failed compute build keeps no block
device record, so `volumes_attached` omits the released root volume.
Server reads while a volume-backed build is held list the reserved root
volume, which has not been checked against a reference deployment. Server detail does not include
`OS-EXT-AZ:availability_zone`, `config_drive`, `OS-SRV-USG` timestamps,
`OS-DCF:diskConfig`, `hostId`, or the access IP fields.

## Verification

Run `go test ./internal/api/server -run 'Boots|ServerCreateAndDelete|BuildControls|BootFromVolume|HeldVolume' -count=1`
and `go test ./internal/app/compute -run TestServers -count=1`. The
Gophercloud tests boot servers on the default network, a network UUID,
and an existing port, wait with `servers.WaitForStatus`, and check detail,
lists, addresses with floating IPs, security groups, key name, metadata,
and the Neutron ports. They delete the servers and check the `404`, the
deleted and unbound ports, floating IP disassociation, and the detached
volume. Raw HTTP checks the `202` response fields and `Location`, the
`400`, `403`, and `409` cases, the locked delete, the `204` delete, and the
build controls before and after each outcome. The volume boot tests use
Gophercloud to boot from an existing volume and from an image, and check
the root attachment, the volume state, the image metadata, the rejected
root detach, and `delete_on_termination` on delete. Raw HTTP checks the
block device `400` cases, `"image": ""`, and the reserved volume of a
held build. The application tests check the validation order, build
failures, and port and volume release against the pinned Nova source.

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
[Nova block device schema]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L21-L85
[Nova block device processing]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/block_device.py#L174-L225
[Gophercloud block device options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L336-L386
[Nova volume boot metadata]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/block_device.py#L615-L695
[Nova boot image checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L745-L869
[Nova block device validation]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L1985-L2074
[Cinder attachment reservation]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L2441-L2470
[Nova volume creation]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/virt/block_device.py#L445-L470
[Cinder attachment view]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/v3/views/volumes.py#L106-L125
[Nova server image view]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/views/servers.py#L621-L650
[Nova root detach check]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/volume_attachments.py#L411-L428
[Nova BDM cleanup]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2695-L2724
[Nova cell0 block devices]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/conductor/manager.py#L1650-L1655
[Nova build cleanup]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L3340-L3368
[Nova block device preparation failure]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L2963-L2969
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
