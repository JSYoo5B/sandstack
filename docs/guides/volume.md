# Block Storage sandbox

Use the Block Storage sandbox to test a client that discovers a volume and
handles a storage error. Authenticate with the [Identity fixture](identity.md).
Gophercloud's `NewBlockStorageV3` selects the public `block-storage` endpoint
in `RegionOne`. The catalog endpoint is `/volume/v3/{project_id}`.

Use `volumes.List` for `GET /volumes/detail` and `volumes.Get` for
`GET /volumes/{volume_id}` on that endpoint. Use the project's `X-Auth-Token`.
The sandbox starts with an unattached `fixture-volume` of 1 GiB and uses
baseline Cinder v3 metadata.

## Discover the default volume type

See [Discover the default volume type](volume-discovery.md#discover-the-default-volume-type).

### List visible volume types

See [List visible volume types](volume-discovery.md#list-visible-volume-types).

## Discover the storage availability zone

See [Discover the storage availability zone](volume-discovery.md#discover-the-storage-availability-zone).

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
outcome. Set `volumes.CreateOpts.Metadata` to include user metadata in
the create response, volume detail, and detailed list. A raw request may
omit `metadata` or set it to `null` for an empty map. It follows the
[volume metadata key and value rules](#read-and-replace-volume-metadata).
See the [Cinder volume create schema] and [Gophercloud volume create options].
The optional `volume_type` accepts the type name or UUID for each
creation path. Without it, a volume created from a snapshot or source
volume inherits that source's type. See the [Cinder volume type
resolution].

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
in `creating` or `extending` returns `400`. An `available`, `error`,
`error_extending`, or `error_restoring` volume can be deleted once its
dependent snapshots are removed. Backups do not block deletion. The
response is `202` with an empty body and the volume is absent from
subsequent reads. Reset discards created volumes and restores the
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

## Set the volume bootable flag

Use `volumes.SetBootable` with `volumes.BootableOpts{Bootable: true}` or
`false`. Gophercloud sends `os-set_bootable` to
`POST /volumes/{volume_id}/action`. An accepted update returns `200`
with an empty body. Use detail or the detailed list to read the string
`bootable` value. See the [Cinder bootable action], [Cinder bootable
schema], and [Gophercloud bootable options].

Both values preserve the volume's status, attachment, user metadata,
and published image metadata. Each accepted action refreshes the
update timestamp and leaves a scheduled
[detail failure](#fail-the-next-volume-detail-read) pending. Pending
provisioning from a Glance image or a bootable source volume applies
its captured bootable flag on successful completion.

## Read and replace volume metadata

Use `GET /volumes/{volume_id}/metadata` to read a volume's user metadata.
Send `PUT` to the same path with a body such as
`{"metadata":{"team/owner":"storage"}}` to replace the whole map. Send
`{"metadata":{}}` to clear it. Both operations return `200` with a
`metadata` object. They use the project token and preserve any volume
status, attachment, and update timestamp. See the [Cinder volume metadata
controller] and [Cinder volume metadata schema].

Keys have 1 to 255 characters from ASCII letters, digits, spaces, and
`-_.:/`. Values are strings of up to 255 characters. An empty string is
valid. Invalid metadata returns `400` and leaves the previous map intact.

Use Gophercloud v2.15.0's `ServiceClient.Get` and `ServiceClient.Put`
with `ServiceURL("volumes", volumeID, "metadata")`. The updated map also
appears in `volumes.Get` and the detailed list. Metadata operations leave
a scheduled [detail failure](#fail-the-next-volume-detail-read) pending.

### Merge volume metadata

Send `POST /volumes/{volume_id}/metadata` with a `metadata` object to
add keys or change their values. Existing keys omitted from the request
retain their values. An empty object keeps the map intact. The response
is `200` with the full merged map. The same validation and state
preservation rules apply. Use Gophercloud v2.15.0's `ServiceClient.Post`
with an explicit `200` success code. See the [merge volume metadata
reference] and [Cinder volume metadata controller].

## Create a volume from a Glance image

See [Create a volume from a Glance image](volume-sources.md#create-a-volume-from-a-glance-image).

## Create a volume from a snapshot

See [Create a volume from a snapshot](volume-sources.md#create-a-volume-from-a-snapshot).

## Clone an existing volume

See [Clone an existing volume](volume-sources.md#clone-an-existing-volume).

## Create and provision a volume snapshot

See [Create and provision a volume snapshot](volume-snapshots.md#create-and-provision-a-volume-snapshot).

### Snapshot a volume in use

See [Snapshot a volume in use](volume-snapshots.md#snapshot-a-volume-in-use).

## Filter and page through the detailed snapshot list

See [Filter and page through the detailed snapshot list](volume-snapshots.md#filter-and-page-through-the-detailed-snapshot-list).

## Update a snapshot name or description

See [Update a snapshot name or description](volume-snapshots.md#update-a-snapshot-name-or-description).

## Read and replace snapshot metadata

See [Read and replace snapshot metadata](volume-snapshots.md#read-and-replace-snapshot-metadata).

### Merge snapshot metadata

See [Merge snapshot metadata](volume-snapshots.md#merge-snapshot-metadata).

## Back up and restore a volume

See [Back up and restore a volume](volume-backups.md).

## Transfer a volume

See [Transfer a volume](volume-transfers.md).

## Upload a volume to an image

See [Upload a volume to an image](volume-upload-image.md).

## Read the attachment state

After [Nova attaches a volume](compute.md#attach-and-detach-a-cinder-volume),
Cinder lists it as `in-use`. Its `attachments` array contains the server ID,
volume ID, assigned device, and attachment timestamp. The `id` is the volume
ID and `attachment_id` identifies this particular attachment. An `in-use`
volume returns `400` on Cinder delete and `409` when the volume fault control
tries to set it to `error`. The fault control also returns `409` for a
volume in `backing-up`, `restoring-backup`, or `awaiting-transfer`. After detach, the volume
becomes `available` with an empty `attachments` array. See the [volume details reference].

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

## Change the volume type

See [Change a volume type](volume-retype.md).

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
reads, deletion, faults, and reset through Gophercloud. The extension
tests check `extending` with the original size, both completion outcomes,
and recovery after a failed extension. The HTTP tests use the
[create volume reference] and [delete volume reference].
The volume update tests check field omission, explicit clearing, validation,
and preservation of an existing attachment.
Detailed list tests exercise Gophercloud filters after volume updates and
check empty results and invalid queries over HTTP.
The transient detail outage test checks the Cinder `503` response and
reads the live volume on retry through Gophercloud.

[create volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#create-a-volume
[Cinder volume create schema]: https://github.com/openstack/cinder/blob/master/cinder/api/schemas/volumes.py#L23-L69
[Cinder volume type resolution]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L360-L375
[Gophercloud volume create options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/volumes/requests.go#L105-L164
[detailed volume list reference]: https://docs.openstack.org/api-ref/block-storage/v3/#list-accessible-volumes-with-details
[Cinder volume list controller]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L142-L180
[Cinder pagination parameters]: https://github.com/openstack/cinder/blob/master/cinder/api/common.py#L35-L103
[Cinder collection links]: https://github.com/openstack/cinder/blob/master/cinder/api/common.py#L270-L323
[Gophercloud volume list options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/volumes/requests.go#L220-L253
[update volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#update-a-volume
[Cinder volume update schema]: https://github.com/openstack/cinder/blob/master/cinder/api/schemas/volumes.py#L82-L110
[Cinder volume update controller]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L438-L460
[Gophercloud volume update options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/volumes/requests.go#L285-L307
[Cinder bootable action]: https://github.com/openstack/cinder/blob/10.0.0/cinder/api/contrib/volume_actions.py#L333-L357
[Cinder bootable schema]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/schemas/volume_actions.py#L145-L159
[Gophercloud bootable options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/volumes/requests.go#L626-L656
[Cinder volume metadata controller]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/volume_metadata.py
[Cinder volume metadata schema]: https://github.com/openstack/cinder/blob/master/cinder/api/schemas/volume_metadata.py
[merge volume metadata reference]: https://docs.openstack.org/api-ref/block-storage/v3/#create-a-volumes-metadata
[delete volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#delete-a-volume
[volume details reference]: https://docs.openstack.org/api-ref/block-storage/v3/#show-a-volume-s-details
[Cinder WSGI fault mapping]: https://github.com/openstack/cinder/blob/master/cinder/api/openstack/wsgi.py#L1203-L1228
[sandbox reset]: reset.md
[extend volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#extend-a-volume-size
[Cinder extension completion]: https://github.com/openstack/cinder/blob/master/cinder/volume/manager.py#L2751-L2784
