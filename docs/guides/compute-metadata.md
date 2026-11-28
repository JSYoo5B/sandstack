# Server metadata in the Compute sandbox

[Compute sandbox](compute.md)

Use the project token and server ID from the Compute fixture to read or
change its metadata with Nova v2.1.

## Read and replace server metadata

Use `servers.Metadata` and `servers.ResetMetadata` in Gophercloud v2.15.0.
These send `GET` and `PUT` to `/servers/{server_id}/metadata`. Both return
`200` with a `metadata` object. Use `servers.MetadataOpts{}` to clear all
keys. A nil map is sent as JSON `null` and returns `400`. See the [Nova
metadata reference], [Nova metadata schema], and [Gophercloud metadata
operations].

Keys contain 1 to 255 ASCII letters, digits, spaces, or `-_.:`. Values are
strings of up to 255 characters, including empty strings. SandStack uses
Nova's default limit of 128 entries per server. Exceeding it returns
`403` and preserves the existing map. See the [Nova metadata quota].

Read metadata in any fixture state. Replace it while the server is
`ACTIVE`, `PAUSED`, `SUSPENDED`, or `SHUTOFF`. A replacement while the
server reports an injected `ERROR` returns `409`. Use the detailed list or
ordinary detail to read the same metadata fields. A stale detail snapshot
keeps the earlier map. The metadata endpoint operates on live state and
leaves a scheduled detail failure pending. SandStack updates the server
timestamp when the map changes. See the [Nova metadata state checks].

### Merge server metadata

Use `servers.UpdateMetadata` with `servers.MetadataOpts` to add or
update selected keys. It sends `POST /servers/{server_id}/metadata` and
returns `200` with the full resulting map. Existing keys omitted from the
request retain their values. An empty object keeps the current map.
The same state and validation rules apply, and the quota applies to the
result after merging. See the [Nova metadata merge reference] and
[Gophercloud metadata merge operation].

### Use an individual metadata item

Use `servers.CreateMetadatum`, `servers.Metadatum`, and
`servers.DeleteMetadatum` for `PUT`, `GET`, and `DELETE` on
`/servers/{server_id}/metadata/{key}`. A successful `GET` or `PUT`
returns `200` with a single-pair `meta` object. The key in the `PUT`
body must match the decoded URL key. It adds or replaces only that item
and follows the same state and quota rules as a collection update.

A missing item returns `404` on `GET` or `DELETE`. An accepted `DELETE`
returns `204` with an empty body and leaves the server timestamp intact.
Deleting an existing item while the server reports an injected `ERROR`
returns `409`. See the [Nova metadata item operations] and [Gophercloud
metadata item operations].

## Verification

Run `go test ./...` to check collection replacement, merging, and item
operations through Gophercloud. Raw HTTP tests check response envelopes,
validation, quota, and encoded item keys.

[Nova metadata reference]: https://docs.openstack.org/api-ref/compute/#server-metadata-servers-metadata
[Nova metadata merge reference]: https://docs.openstack.org/api-ref/compute/#create-or-update-metadata-items
[Nova metadata schema]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/schemas/server_metadata.py#L42-L49
[Nova metadata state checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5558-L5571
[Nova metadata quota]: https://docs.openstack.org/nova/latest/configuration/config.html#quota.metadata_items
[Gophercloud metadata operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L886-L930
[Gophercloud metadata merge operation]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L934-L949
[Nova metadata item operations]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/server_metadata.py#L73-L152
[Gophercloud metadata item operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L951-L1007
