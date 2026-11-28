# Snapshot servers in the Compute sandbox

[Compute sandbox](compute.md)

Use Nova v2.1 `createImage` to snapshot a server into a Glance image,
wait until the image is `active`, and boot a new server from it. Start
with a server from [Boot and delete servers](compute-servers.md).

## Create a snapshot image

Use `servers.CreateImage` with `servers.CreateImageOpts` in Gophercloud
v2.15.0. It sends `POST /servers/{server_id}/action` with a
`createImage` object. `name` is required and follows the same rules as a
[rename](compute.md#rename-a-server). `metadata` is optional and follows
the [metadata rules](compute-metadata.md). Other fields return `400`. See
the [Nova createImage schema] and [Gophercloud createImage].

Nova v2.1 answers `202` with an empty body and a `Location` header that
points to the image under the catalog's Glance endpoint, such as
`http://127.0.0.1:8080/image/images/{image_id}`. Nova returns the
`image_id` body only from microversion 2.45. `ExtractImageID` reads the
last segment of `Location` at v2.1. See the [Nova createImage handler]
and [Nova image URL].

Nova records a `createImage` [server action](compute-actions.md).

### Rejected requests

| Condition | Status |
| --- | --- |
| Unknown server | `404` |
| More than 128 `metadata` items | `403` with `Image metadata limit exceeded` |
| Server not `ACTIVE`, `SHUTOFF`, `PAUSED`, or `SUSPENDED` | `409` |
| Another snapshot pending on the server | `409` |

The state conflicts use Nova's message `Cannot 'createImage' instance
{server_id} while it is in vm_state {state}`, with `building` or `error`,
or `task_state image_snapshot_pending`. Nova does not check the server
lock for snapshots, so a locked server can be snapshotted. A rejected
request creates no image and records no action. See the [Nova metadata
quota check], [Nova snapshot state check], and [Nova state conflict].

## Image-backed servers

Nova creates a `queued` private image owned by the project with these
properties:

- `instance_uuid`, `user_id` of the token's user, and `image_type`
  `snapshot`.
- `base_image_ref`, the boot image ID. A server booted from a snapshot
  keeps the original `base_image_ref`.
- The boot image's other properties, except `cache_in_nova`,
  `bittorrent`, the `cinder_encryption_*` and `img_signature*` keys, and
  keys starting with `os_glance`.
- The request `metadata`, which overrides the properties above.

`min_ram` comes from the boot image. `min_disk` is the larger of the
boot image's `min_disk` and the flavor's root disk. Nova reads these
values from what it recorded when it built the server. See the [Nova
snapshot API], [Nova snapshot image creation], [Nova system metadata
mapping], and [Nova inherited properties].

When the snapshot completes, the image becomes `active` with
`container_format` `bare` or the boot image's container format,
`disk_format` `qcow2`, the boot image's bytes, size, and checksum, and the
libvirt driver properties `image_location` `snapshot`, `image_state`
`available`, and `owner_id`. If the boot image was deleted, the image has
no bytes. See the [libvirt snapshot metadata].

### Wait for the snapshot

SandStack completes an image-backed snapshot before it answers, as it does
for [builds](compute-servers.md#wait-for-the-server). The first
`images.Get` reports `active`. Nova snapshots asynchronously.

To observe a pending snapshot, hold snapshots started afterwards with the
dedicated admin token, then complete or fail each one.

```sh
controls="http://127.0.0.1:8080/_sandstack/compute"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$controls/hold-images"
# Snapshot the server with the Compute API, then:
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" \
  "$controls/servers/${server_id}/complete-image"
```

While held, the image is `queued` without formats, and the server
reports `OS-EXT-STS:task_state` `image_snapshot_pending`. Another
`createImage`, `os-stop`, `os-start`, `reboot`, `pause`, `unpause`,
`suspend`, or `resume` returns `409`. `complete-image` activates the
image and clears the task state. `fail-image` deletes the image and
clears the task state, as Nova's compute manager does when the snapshot
fails. Deleting the server deletes the pending image. Each control
returns `204`, `409` when the server has no pending snapshot, and `404`
for an unknown server. Send `DELETE` to `hold-images` to complete later
snapshots immediately again. See the [Nova snapshot manager] and [Nova
image cleanup].

## Volume-backed servers

For a server that [boots from a volume](compute-servers.md#boot-from-a-volume),
Nova forces a Cinder snapshot of each attached volume, starting with the
root volume. Each snapshot is named `snapshot for {image name}`, takes the
volume's description, and stays `creating` until its
[Cinder control](volume-snapshots.md) completes it. Nova does not wait
for the snapshots. See the [Nova volume-backed snapshot].

The Glance image is `active` at once with size `0`, `disk_format` `raw`,
`container_format` `bare`, and the checksum of empty data, because Nova
activates it with an empty upload. Its properties are the inherited boot
properties with an empty `base_image_ref`, the request `metadata`,
`root_device_name` `/dev/vda`, `bdm_v2` `True`, and
`block_device_mapping`, a JSON array with one item per snapshot. Nova does
not add `instance_uuid`, `user_id`, or `image_type` to this image. See the
[Nova empty image activation].

Each `block_device_mapping` item has `source_type` `snapshot`,
`destination_type` `volume`, `snapshot_id`, `device_name`, `volume_size`
from the volume, `boot_index` `0` and the server's
`delete_on_termination` for the root volume, and `null` and `false` for
other volumes. Nova writes the remaining block device fields as `null`.
SandStack sets `disk_bus` `virtio` and `device_type` `disk`, the values
the libvirt driver records, and orders the keys alphabetically. Nova's key
order varies. These values have not been checked against a reference
deployment. See the [Nova snapshot mapping] and [Nova block device
fields].

## Boot from the snapshot

Wait with `images.Get` until the image is `active`, then pass its ID as
`ImageRef` to `servers.Create`. A server booted from an image-backed
snapshot reports the snapshot as its image, and its own snapshots inherit
the snapshot's properties. Booting from a volume-backed snapshot image,
which needs block device mappings from snapshots, returns `400` with
`Booting from an image with a block device mapping is not supported.`
Glance shows additional properties as top-level image fields, which
Gophercloud reads into `Image.Properties`. See the [Glance image view].

[Reset the sandbox](reset.md) removes snapshot images and stops holding
snapshots.

## Omissions

SandStack does not model the compute task states after
`image_snapshot_pending` (`image_snapshot`, `image_pending_upload`, and
`image_uploading`), the Glance `saving` status, quiescing, the Cinder
snapshot quota check, image members for snapshots by another project, or
the `owner_project_name` and `owner_user_name` properties. The `image_id`
response body of
microversion 2.45 is not available because SandStack serves v2.1.

## Verification

Run `go test ./internal/api/server -run 'Snapshot|CreateImage' -count=1`
and `go test ./internal/app/compute -run 'TestServersSuite/.*Image' -count=1`.
The Gophercloud tests snapshot an image-backed server, wait with
`images.Get`, check the image fields and properties and the
`createImage` action, boot a server from the snapshot, and delete both
servers and the image. They also snapshot a volume-backed server and
check the image, the `block_device_mapping`, the Cinder snapshot, and the
rejected boot. Raw HTTP checks the `202` response with its empty body and
`Location`, the action record, the `400`, `403`, `404`, and `409` cases,
and the hold controls before and after each outcome. The application
tests check inherited and non-inheritable properties, the state checks
and quota, the locked server, pending snapshot cleanup, and the volume
mapping against the pinned Nova source.

[Nova createImage schema]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L484-L499
[Gophercloud createImage]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L1025-L1085
[Nova createImage handler]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1425-L1488
[Nova image URL]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/image/glance.py#L1101-L1107
[Nova metadata quota check]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L282-L289
[Nova snapshot state check]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L142-L180
[Nova state conflict]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L330-L346
[Nova snapshot API]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3454-L3511
[Nova snapshot image creation]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/utils.py#L1253-L1335
[Nova system metadata mapping]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/utils.py#L536-L590
[Nova inherited properties]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/utils.py#L64-L77
[libvirt snapshot metadata]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/virt/libvirt/driver.py#L3184-L3206
[Nova snapshot manager]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L4583-L4697
[Nova image cleanup]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L219-L237
[Nova volume-backed snapshot]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3513-L3653
[Nova empty image activation]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/image/glance.py#L523-L675
[Nova snapshot mapping]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/block_device.py#L302-L313
[Nova block device fields]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/block_device.py#L51-L90
[Glance image view]: https://github.com/openstack/glance/blob/12.0.0/glance/api/v2/images.py#L736-L760
