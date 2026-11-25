# Block Storage sandbox

Use the Block Storage sandbox to test a client that discovers a volume and
handles a storage error. Authenticate with the [Identity fixture](identity.md).
Gophercloud's `NewBlockStorageV3` selects the public `block-storage` endpoint
in `RegionOne`. The catalog endpoint is `/volume/v3/{project_id}`.

Use `volumes.List` for `GET /volumes/detail` and `volumes.Get` for
`GET /volumes/{volume_id}` on that endpoint. Use the project's `X-Auth-Token`.
The sandbox starts with an unattached `fixture-volume` of 1 GiB and uses
baseline Cinder v3 metadata.

## Filter the detailed volume list

`GET /volumes/detail` accepts `name` and `status` filters for the token's
project. Each field matches the current value exactly. When both filters
are supplied, both must match. Changes to a volume's name or status affect
subsequent list results. A list with no matches returns `{"volumes":[]}`.
See the [detailed volume list reference] and [Cinder volume list controller].

In Gophercloud v2.15.0, pass `Name` and `Status` in `volumes.ListOpts` to
`volumes.List`. Use a raw `?name=` query to match an explicit empty name.
An omitted filter leaves its field unrestricted. Repeated or unknown query
keys return `400` on the SandStack detailed list.

## Page through the detailed volume list

Use `limit` and `marker` for forward pagination. SandStack shows volumes
in ascending ID order and accepts `sort=id:asc` to request that order
explicitly. The marker must identify a volume visible to the token's
project, even when the current filters exclude it. The page starts after
that ID. An unknown or inaccessible marker returns `404`. The default
and maximum page size are 1,000. A raw `limit=0` returns an empty page.
See the [detailed volume list reference] and [Cinder pagination parameters].

A nonempty page that fills the effective limit contains a `next` link in
`volumes_links`, even if it holds the last matching volume. The link
uses the last ID as its marker and carries any name, status, and sort
parameters. If a follow-up page is empty, it has no `next` link. See the
[Cinder collection links].

In Gophercloud v2.15.0, set `Limit`, `Marker`, or `Sort: "id:asc"` in
`volumes.ListOpts`. The pager from `volumes.List` follows `next` links
when you call `AllPages`. See the [Gophercloud volume list options].

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

## Update a volume name or description

Send `PUT /volumes/{volume_id}` on the project endpoint with a `volume`
object. The sandbox accepts `name` and `description` and returns `200`
with the updated volume. Each field accepts a string of up to 255
characters. SandStack trims surrounding whitespace. Omit a field to keep
its value. An empty `volume` object is a no-op. A raw request can set a
field to JSON `null` to clear it to `null`. See the [update volume
reference], [Cinder volume update schema], and [Cinder volume update
controller].

In Gophercloud v2.15.0, pass `volumes.UpdateOpts` to `volumes.Update`.
Leave `Name` or `Description` nil to omit it, or pass a pointer to an
empty string to write `""` explicitly. The update preserves volume status
and attachments, including when the volume is `in-use`. See the
[Gophercloud volume update options].

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
the source volume ID as `volume_id`. The request can also include a name
and description. For an `available` volume, omit `force` or set it to
`false`. A `202` response contains a snapshot in `creating` status with
the source volume ID and size.
Use `snapshots.Get` for `GET /snapshots/{snapshot_id}` and
`snapshots.ListDetail` for `GET /snapshots/detail` to observe the outcome.
Both reads include the owning project ID and build progress. Progress is
`100%` after successful completion and `0%` while pending or after a
fixture failure.

Use `snapshots.CreateOpts.Metadata` in Gophercloud v2.15.0 to set initial
metadata for either normal or forced snapshot creation. Each key has 1 to
255 characters from ASCII letters, digits, spaces, hyphens, underscores,
periods, colons, and slashes. Each value is a string of up to 255
characters and may be empty. Omit `metadata` or set that field to `{}`
or `null` in a raw request to start with an empty map. The snapshot keeps
its initial metadata through provisioning. See the [Cinder snapshot create schema],
[Cinder create metadata parameter type], and [Gophercloud snapshot create
options].

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

### Snapshot a volume in use

For a volume attached to a Nova server, set `Force: true` in Gophercloud
v2.15.0 `snapshots.CreateOpts`, or send the JSON boolean `force: true` in
the raw `snapshot` object. SandStack uses the Cinder v3.0 behavior. An
`in-use` source with `force` omitted or set to `false` returns `400`.
Cinder microversion 3.66 allows snapshots of attached volumes without
the flag. See the [create snapshot reference], [Cinder snapshot force
handling], and [Cinder snapshot microversion].

The request returns `202` with a new snapshot in `creating`. Use the same
completion or failure controls as for an available source. These controls
apply to the snapshot. The source volume remains `in-use` and its Nova
attachment stays in place throughout the operation. See the [Gophercloud
snapshot create options].

## Filter and page through the detailed snapshot list

`GET /snapshots/detail` lists snapshots in the token's project. Filter by
`name`, `status`, or both. The supplied fields must match the current
snapshot values exactly, so renames and provisioning outcomes affect the
next request. A raw `name=` matches a stored empty name. A list with no
matches returns `{"snapshots":[]}`. See the [snapshot list reference],
[Cinder snapshot list controller], and [Cinder snapshot list query].

Snapshots appear in ascending ID order, which you can request explicitly
with `sort=id:asc`. Use `limit` and `marker` to page forward. The marker
can identify any snapshot visible in the current project, including one
outside the current filters. An unknown or inaccessible marker returns
`404`. The default and maximum page size are 1,000. A raw `limit=0` returns
an empty page. See the [Cinder pagination parameters].

A nonempty page that fills its limit contains a `next` link in
`snapshots_links`, even if the page holds the final matching snapshot.
The link uses the last snapshot ID as the marker and preserves the page
size, filters, and sort. An empty follow-up has no `next` link. In
Gophercloud v2.15.0, pass `Name`, `Status`, `Limit`, `Marker`, and `Sort` in
`snapshots.ListOpts` to `snapshots.ListDetail`. Its `AllPages` follows
the links. See the [Cinder collection links] and [Gophercloud snapshot
list options].

## Update a snapshot name or description

Send `PUT /snapshots/{snapshot_id}` on the project endpoint with a
`snapshot` object containing `name`, `description`, or both. Omitting a
field preserves its value. An empty object returns `400`. Each string
must be at most 255 characters before SandStack trims surrounding
whitespace. An explicit empty string writes `""`, while JSON `null`
clears the field to `null`. See the [update snapshot reference],
[Cinder snapshot update schema], and [Cinder snapshot update controller].

In Gophercloud v2.15.0, pass `snapshots.UpdateOpts` to `snapshots.Update`.
Use a nil field pointer to omit the field or a pointer to an empty string
to write `""`. Use a raw request to send JSON `null`. This update works
for snapshots in `creating`, `available`, or `error` and preserves their
status, source volume ID, and size. See the [Gophercloud snapshot update options].

The `200` response shows the new name or description with the previous
`updated_at` value. Read the snapshot again to observe the persisted
timestamp, as Cinder returns the saved snapshot without reloading it.
See the [Cinder snapshot save implementation].

## Read and replace snapshot metadata

Use `GET /snapshots/{snapshot_id}/metadata` on the project endpoint to
read the metadata map. `PUT` to the same path replaces the entire map.
Both return `200` with a `metadata` object. Send `{"metadata":{}}` in a
raw `PUT` to clear all entries. A snapshot absent from the token's
project returns `404`. See the [read snapshot metadata reference],
[replace snapshot metadata reference], and [Cinder snapshot metadata controller].

Keys accept 1 to 255 characters from ASCII letters, digits, spaces,
hyphens, underscores, periods, and colons. Values must be strings of at
most 255 characters and may be empty. A malformed payload or JSON `null`
returns `400`.
See the [Cinder snapshot metadata schema].

In Gophercloud v2.15.0, pass a nonempty `Metadata` map in
`snapshots.UpdateMetadataOpts` to `snapshots.UpdateMetadata`. The SDK
omits an empty map, so use a raw request to clear it. Use
`ServiceClient.Get` to read the metadata collection. Snapshot detail and
detailed list responses also include the updated metadata. Replacing the
metadata preserves snapshot status and `updated_at` in the fixture.
See the [Gophercloud snapshot
metadata options].

### Merge snapshot metadata

Send `POST /snapshots/{snapshot_id}/metadata` with a `metadata` object to
add or overwrite the supplied keys. The `200` response contains the
complete metadata map. Omitted keys keep their values, including keys
with slashes set when the snapshot was created. Incoming keys and values
follow the same validation as `PUT`. An empty object in `metadata` is a
no-op. The fixture preserves snapshot status and `updated_at`. See the
[merge snapshot metadata reference], [Cinder snapshot metadata controller],
and [Cinder snapshot metadata service].

In Gophercloud v2.15.0, use `ServiceClient.Post` with `200` in `OkCodes`.
The SDK's `snapshots.UpdateMetadata` sends `PUT` for full replacement.

```go
body := map[string]map[string]string{
    "metadata": {"environment": "test"},
}
var result struct {
    Metadata map[string]string `json:"metadata"`
}
_, err := client.Post(ctx, client.ServiceURL("snapshots", snapshotID, "metadata"),
    body, &result, &gophercloud.RequestOpts{OkCodes: []int{http.StatusOK}})
```

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

## Fail the next volume detail read

Send `PUT` to `/_sandstack/volume/volumes/{volume_id}/unavailable-detail`
with `X-SandStack-Admin-Token` to schedule one failure for that volume.
The control returns `204`. The next authorized `GET /volumes/{volume_id}`
for the selected volume returns `503` with a `serviceUnavailable` error.
Retry `volumes.Get` with the same project token to read the volume's
current state. See the [Cinder WSGI fault mapping].

Detailed lists, detail reads of other volumes, and rejected requests
leave the failure pending. Repeating `PUT` before a failure is consumed
keeps one failure scheduled. Send `DELETE` to the same control with the
admin token for a `204` and cancel the pending failure. A [sandbox reset]
also clears pending failures.

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
The volume update tests check field omission, explicit clearing, validation,
and preservation of an existing attachment.
The snapshot update tests check partial changes, preserved source state,
and the timestamp returned by later reads.
Detailed list tests exercise Gophercloud filters after volume updates and
check empty results and invalid queries over HTTP.
The transient detail outage test checks the Cinder `503` response and
reads the live volume on retry through Gophercloud.
The [Cinder snapshot lookup], [Cinder size default], and
[Cinder volume creation flow] describe source and size validation.

[create volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#create-a-volume
[detailed volume list reference]: https://docs.openstack.org/api-ref/block-storage/v3/#list-accessible-volumes-with-details
[Cinder volume list controller]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L142-L180
[Cinder pagination parameters]: https://github.com/openstack/cinder/blob/master/cinder/api/common.py#L35-L103
[Cinder collection links]: https://github.com/openstack/cinder/blob/master/cinder/api/common.py#L270-L323
[Gophercloud volume list options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/volumes/requests.go#L220-L253
[update volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#update-a-volume
[Cinder volume update schema]: https://github.com/openstack/cinder/blob/master/cinder/api/schemas/volumes.py#L82-L110
[Cinder volume update controller]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L438-L460
[Gophercloud volume update options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/volumes/requests.go#L285-L307
[delete volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#delete-a-volume
[volume details reference]: https://docs.openstack.org/api-ref/block-storage/v3/#show-a-volume-s-details
[Cinder WSGI fault mapping]: https://github.com/openstack/cinder/blob/master/cinder/api/openstack/wsgi.py#L1203-L1228
[sandbox reset]: reset.md
[create snapshot reference]: https://docs.openstack.org/api-ref/block-storage/v3/#create-a-snapshot
[Cinder snapshot create schema]: https://github.com/openstack/cinder/blob/master/cinder/api/schemas/snapshots.py#L20-L39
[Cinder create metadata parameter type]: https://github.com/openstack/cinder/blob/master/cinder/api/validation/parameter_types.py#L137-L188
[Cinder snapshot force handling]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/snapshots.py#L174-L211
[Cinder snapshot microversion]: https://github.com/openstack/cinder/blob/master/cinder/api/microversions.py#L164
[Gophercloud snapshot create options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/snapshots/requests.go#L12-L37
[update snapshot reference]: https://docs.openstack.org/api-ref/block-storage/v3/#update-a-snapshot
[Cinder snapshot update schema]: https://github.com/openstack/cinder/blob/master/cinder/api/schemas/snapshots.py#L42-L64
[Cinder snapshot update controller]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/snapshots.py#L213-L236
[Cinder snapshot save implementation]: https://github.com/openstack/cinder/blob/master/cinder/objects/snapshot.py#L175-L202
[read snapshot metadata reference]: https://docs.openstack.org/api-ref/block-storage/v3/#show-a-snapshot-s-metadata
[replace snapshot metadata reference]: https://docs.openstack.org/api-ref/block-storage/v3/#update-a-snapshot-s-metadata
[merge snapshot metadata reference]: https://docs.openstack.org/api-ref/block-storage/v3/#create-a-snapshot-s-metadata
[Cinder snapshot metadata controller]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/snapshot_metadata.py#L28-L91
[Cinder snapshot metadata schema]: https://github.com/openstack/cinder/blob/master/cinder/api/schemas/snapshot_metadata.py#L13-L50
[Cinder snapshot metadata service]: https://github.com/openstack/cinder/blob/master/cinder/volume/api.py#L1279-L1306
[Gophercloud snapshot metadata options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/snapshots/requests.go#L177-L208
[Gophercloud snapshot update options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/snapshots/requests.go#L149-L170
[snapshot details reference]: https://docs.openstack.org/api-ref/block-storage/v3/#show-a-snapshot-s-details
[snapshot list reference]: https://docs.openstack.org/api-ref/block-storage/v3/#list-snapshots-and-details
[Cinder snapshot list controller]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/snapshots.py#L111-L152
[Cinder snapshot list query]: https://github.com/openstack/cinder/blob/master/cinder/db/api.py#L3724-L3771
[Gophercloud snapshot list options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/snapshots/requests.go#L72-L130
[delete snapshot reference]: https://docs.openstack.org/api-ref/block-storage/v3/#delete-a-snapshot
[extend volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#extend-a-volume-size
[Cinder extension completion]: https://github.com/openstack/cinder/blob/master/cinder/volume/manager.py#L2751-L2784
[Cinder snapshot lookup]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L369-L375
[Cinder size default]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L408-L414
[Cinder volume creation flow]: https://github.com/openstack/cinder/blob/master/cinder/volume/flows/api/create_volume.py#L127-L205
