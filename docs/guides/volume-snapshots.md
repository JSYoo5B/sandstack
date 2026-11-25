# Snapshots in the Block Storage sandbox

[Block Storage sandbox](volume.md)

[Create and provision an available volume](volume.md#create-and-provision-a-volume)
before creating a snapshot.

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
snapshot to [create a new volume](volume-sources.md#create-a-volume-from-a-snapshot).

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

## Verification

Run `go test ./...` to exercise snapshot operations through Gophercloud.
The snapshot tests exercise both provisioning outcomes and verify that
deleting a dependent snapshot releases its source volume for deletion.
The snapshot update tests check partial changes, preserved source state,
and the timestamp returned by later reads. The HTTP tests use the
[create snapshot reference], [snapshot details reference],
[snapshot list reference], and [delete snapshot reference].

[Cinder pagination parameters]: https://github.com/openstack/cinder/blob/master/cinder/api/common.py#L35-L103
[Cinder collection links]: https://github.com/openstack/cinder/blob/master/cinder/api/common.py#L270-L323
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
