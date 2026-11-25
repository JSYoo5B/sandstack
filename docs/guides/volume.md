# Block Storage sandbox

Use the Block Storage sandbox to test a client that discovers a volume and
handles a storage error. Authenticate with the [Identity fixture](identity.md).
Gophercloud's `NewBlockStorageV3` selects the public `block-storage` endpoint
in `RegionOne`. The catalog endpoint is `/volume/v3/{project_id}`.

Use `volumes.List` for `GET /volumes/detail` and `volumes.Get` for
`GET /volumes/{volume_id}` on that endpoint. Use the project's `X-Auth-Token`.
The sandbox starts with an unattached `fixture-volume` of 1 GiB. The
scenario uses baseline Cinder v3 metadata and unfiltered reads.

## Create and provision a volume

Use `volumes.Create` for `POST /volumes` on the project endpoint. The
`volume` object accepts a name and description. For a blank volume, supply
a positive integer size in GiB. The default type is `standard` and the
availability zone is `nova`. The request returns `202` with a volume in
status `creating`. Both reads show that status until the test drives an
outcome.

To make the volume available, take the ID from the create response and
call the SandStack control with the admin token.

```sh
complete_url="http://127.0.0.1:8080/_sandstack/volume/volumes/${volume_id}/complete-create"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$complete_url"
```

The control returns `204` and moves the volume to `available`. Repeating
it for an available volume keeps its update timestamp. To exercise a
failed create, inject the volume fault instead to move it to `error`.

Use `volumes.Delete` for `DELETE /volumes/{volume_id}`. Deleting a volume
in `creating` or `extending` returns `400`. An `available`, `error`, or
`error_extending` volume can be deleted once its dependent snapshots are
removed. The response is `202` with an empty body and the volume is absent
from subsequent reads. Reset discards created volumes and restores the
initial fixture.

## Create a volume from a snapshot

Pass an `available` snapshot from the same project as `SnapshotID` in
Gophercloud's `volumes.CreateOpts`. Leave `Size` unset to inherit the
snapshot's size. A raw request can omit `size` or set it to JSON `null`.
An explicit size must be an integer in GiB at least as large as the
snapshot. A snapshot absent from the project returns `404`. A snapshot in
`creating` or `error` status, or an undersized request, returns `400`.

The `202` response contains a new volume in `creating` status. The create
response, volume detail, and detailed list expose the new volume's size
and the originating `snapshot_id`. Use the
[volume provisioning controls](#create-and-provision-a-volume) to complete
the new volume or simulate a provisioning failure. The new volume keeps
its `snapshot_id` in either outcome. This scenario models provisioning and
source metadata.

## Create and provision a volume snapshot

Use `snapshots.Create` for `POST /snapshots` on the project endpoint. Supply
the ID of an `available` volume as `volume_id`. The request can also include
a name and description. Omit `force` or set it to `false`. A `202` response
contains a snapshot in `creating` status with the source volume ID and size.
Use `snapshots.Get` for `GET /snapshots/{snapshot_id}` and
`snapshots.ListDetail` for `GET /snapshots/detail` to observe the outcome.
The detailed list is unfiltered. Both reads include the owning project ID
and build progress. Progress is `100%` after successful completion and
`0%` while pending or after a fixture failure.

Take the snapshot ID from the create response and send `PUT` to one of the
SandStack controls with the dedicated admin token.

```sh
snapshot_url="http://127.0.0.1:8080/_sandstack/volume/snapshots/${snapshot_id}"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$snapshot_url/complete-create"
```

`complete-create` moves the snapshot to `available`. Use `fail-create`
instead to move it to `error`. The first call and repeats of the same
outcome return `204`. Requesting the other outcome after provisioning
finishes returns `409`.

Use `snapshots.Delete` for `DELETE /snapshots/{snapshot_id}` once the
snapshot reaches `available` or `error`. The response is `202` and the
snapshot disappears from both reads. Deleting a snapshot in `creating`
status or a source volume with any dependent snapshot returns `400`.
[Reset the sandbox](reset.md) to discard the snapshots and restore the
seeded volume.

This scenario models the snapshot lifecycle in metadata. Use an available
snapshot to [create a new volume](#create-a-volume-from-a-snapshot).

## Read the attachment state

After [Nova attaches a volume](compute.md#attach-and-detach-a-cinder-volume),
Cinder lists it as `in-use`. Its `attachments` array contains the server ID,
volume ID, assigned device, and attachment timestamp. The `id` is the volume
ID and `attachment_id` identifies this particular attachment. An `in-use`
volume returns `400` on Cinder delete and `409` when the volume fault control
tries to set it to `error`. After detach, the volume becomes `available` with
an empty `attachments` array. See the [volume details reference].

## Extend an available volume

Use `volumes.ExtendSize` with `volumes.ExtendSizeOpts{NewSize: sizeGiB}`.
The client sends `POST /volumes/{volume_id}/action` with the `os-extend`
action. Start with an `available` volume and request an integer size in GiB
larger than its current size. The request returns `202` with an empty body.
Volume detail and detailed list show `extending` with the original size
until the test drives an outcome.

Send `PUT` to a SandStack control with the dedicated admin token.

```sh
volume_url="http://127.0.0.1:8080/_sandstack/volume/volumes/${volume_id}"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$volume_url/complete-extend"
```

`complete-extend` makes the requested size visible and returns the volume
to `available`. Use `fail-extend` to enter `error_extending` and keep the
original size. Both controls return `204` for a pending extension and `409`
when there is no pending extension. To retry after a failure, clear it with
`DELETE` on the [volume fault control](#inject-and-clear-a-volume-fault),
or [reset the sandbox](reset.md).

The fixture covers offline extension and its metadata transitions. See the
[extend volume reference] and [Cinder extension completion].

## Inject and clear a volume fault

Use the volume ID from the detailed list, or the seeded ID in this example.

```sh
volume_id=7b37635e-be9b-4bfb-bc2e-d163b75921e6
fault_url="http://127.0.0.1:8080/_sandstack/volume/volumes/${volume_id}/fault"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$fault_url"
```

The volume moves from `available` to `error`. Both reads still return
`200`, preserve its ID and size, and show the updated status. Send `DELETE`
to the control URL to restore `available` from `error` or `error_extending`.
During a pending extension, `PUT` on the fault control returns `409`. Use
`fail-extend` to make the extension fail.
[Reset the sandbox](reset.md) to restore the original values and timestamps.

## Verification

Run `go test ./...` to exercise discovery, creation, provisioning, volume
reads, deletion, faults, and reset through Gophercloud. The snapshot tests
exercise both provisioning outcomes and verify that deleting a dependent
snapshot releases its source volume for deletion. Volume creation from a
snapshot is checked for size inheritance, explicit larger sizes, source
validation, and state transitions. The extension tests check `extending`
with the original size, both completion outcomes, and recovery after a
failed extension. The HTTP tests use the
[create volume reference], [delete volume reference],
[create snapshot reference], [snapshot details reference],
[snapshot list reference], and [delete snapshot reference].
The [Cinder snapshot lookup], [Cinder size default], and
[Cinder volume creation flow] describe source and size validation.

[create volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#create-a-volume
[delete volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#delete-a-volume
[volume details reference]: https://docs.openstack.org/api-ref/block-storage/v3/#show-a-volume-s-details
[create snapshot reference]: https://docs.openstack.org/api-ref/block-storage/v3/#create-a-snapshot
[snapshot details reference]: https://docs.openstack.org/api-ref/block-storage/v3/#show-a-snapshot-s-details
[snapshot list reference]: https://docs.openstack.org/api-ref/block-storage/v3/#list-snapshots-and-details
[delete snapshot reference]: https://docs.openstack.org/api-ref/block-storage/v3/#delete-a-snapshot
[extend volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#extend-a-volume-size
[Cinder extension completion]: https://github.com/openstack/cinder/blob/master/cinder/volume/manager.py#L2751-L2784
[Cinder snapshot lookup]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L369-L375
[Cinder size default]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L408-L414
[Cinder volume creation flow]: https://github.com/openstack/cinder/blob/master/cinder/volume/flows/api/create_volume.py#L127-L205
