# Compute sandbox

Use the Compute sandbox to test how a client reacts when a running server
reports an instance fault. Authenticate using the
[Identity fixture](identity.md). Use the public `compute` endpoint in
`RegionOne` from the service catalog. `GET /servers/detail` lists the
project's `fixture-server`. Use its ID to read `GET /servers/{server_id}`.
In Gophercloud, `NewComputeV2` selects the endpoint and `servers.List` and
`servers.Get` read these resources.

The catalog uses `/compute/v2.1/{project_id}`. The project-qualified and
projectless URLs accept an `X-Auth-Token` from the fixture project. The
responses use baseline Nova v2.1 fields for server identity, ownership,
timestamps, and state. The image ID refers to the [Glance fixture](image.md).
The flavor ID refers to the [Nova flavor fixture](#read-the-server-flavor).

## Read the server flavor

After `servers.Get`, pass the returned `Flavor["id"]` to `flavors.Get`
using the same authenticated Compute client. Nova v2.1 answers
`GET /flavors/{flavor_id}` with `200` and a `flavor` object. Fixture
flavor `1` is the public, enabled `m1.tiny` with one vCPU, 512 MiB RAM,
and 1 GiB root disk. It has no ephemeral disk or swap. Nova v2.1 serializes
zero swap as an empty string, which Gophercloud decodes as zero. Unknown
flavors return `404`. See the [Nova flavor reference], [Nova flavor
response], and [Gophercloud flavor lookup].

## Filter the detailed server list

The detailed list supports a single `status` filter. Use `?status=ACTIVE`,
`?status=ERROR`, or `?status=SHUTOFF` to select a state. Values match
without regard to case. An empty, nonmatching, or unknown status produces
an empty `servers` array.

Set `servers.ListOpts.Name` to a regular expression. An unanchored pattern
matches a substring of the current display name. Use `^fixture-server$`
to match the whole name. When both filters are present the server must
match both. An omitted filter leaves its field unrestricted. SandStack uses
Go's regular expression syntax. Nova evaluates patterns in its database,
where the supported syntax can vary. An empty or malformed name pattern,
a repeated parameter, or an unrelated query key returns `400`. See the
[Compute list reference] and [Gophercloud list options].

## Rename a server

Use `servers.Update` with the fixture server ID and
`servers.UpdateOpts{Name: "new name"}` in Gophercloud v2.15.0. It sends
`PUT /servers/{server_id}` with `{"server":{"name":"new name"}}`.
SandStack supports the name field. A name must have 1 to 255 printable
characters in Nova's supported range, with no leading or trailing whitespace.
An empty `server` object keeps the name and update timestamp. Invalid
requests return `400`. See the [Nova update reference], [Nova update schema],
[Nova name validation], and [Gophercloud update options].

The `200` response contains the updated server in Nova's v2.1 update
format. Use detail and detailed list to read its extended power state and
attached volumes. The update preserves the power state, faults, and
attachments. It returns live state even while a detail snapshot or one-shot
detail outage is set. See the [Nova update response].

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
`ACTIVE` or `SHUTOFF`. A replacement while the server reports an injected
`ERROR` returns `409`. Use the detailed list or ordinary detail to read
the same metadata fields. A stale detail snapshot keeps the earlier map.
The metadata endpoint operates on live state and leaves a scheduled
detail failure pending. SandStack updates the server timestamp when the
map changes. See the [Nova metadata state checks].

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

## Stop and start a server

Use the fixture server ID with `servers.Stop` and `servers.Start` in
Gophercloud v2.15.0. Both send `POST /servers/{server_id}/action` under
the same project-scoped Compute endpoint. Send `{"os-stop":null}` to
stop or `{"os-start":null}` to start. A successful action returns `202`
with an empty body. See the [Nova stop reference], [Nova start reference],
and [Gophercloud server actions].

Stop changes `ACTIVE` or `ERROR` to `SHUTOFF` and clears the current
instance fault. Live reads report `OS-EXT-STS:vm_state` as `stopped` and
`OS-EXT-STS:power_state` as `4`. Start changes `SHUTOFF` to `ACTIVE`.
Repeating either action or calling it in an unsupported state returns
`409`. SandStack completes each accepted transition before it responds.
Check the new state on the same server through detail or the detailed
list. Both actions preserve existing Cinder volume attachments. The stale
detail snapshot and the next-detail-read outage controls still apply to
subsequent detail reads.

## Reboot a server

Use `servers.Reboot` with `servers.SoftReboot` or `servers.HardReboot`
in Gophercloud v2.15.0. It sends `POST /servers/{server_id}/action` with
`{"reboot":{"type":"HARD"}}` or its soft equivalent. Nova v2.1 accepts
uppercase, title case, and lowercase spellings. Unknown types and extra
fields return `400`. An accepted action returns `202` with an empty body.
See the [Nova reboot reference], [Nova reboot schema], and [Gophercloud
reboot options].

Soft reboot requires `ACTIVE`. Hard reboot accepts `ACTIVE`, `SHUTOFF`,
or `ERROR`. A soft reboot in either of the latter states returns `409`
and preserves the state. A successful hard reboot can recover the
injected instance fault. SandStack completes the reboot before
responding, so live detail and detailed list show `ACTIVE` immediately.
Nova runs reboots asynchronously. Existing attachments, stale detail
snapshots, and scheduled detail failures remain in place. See the
[Nova reboot state checks].

## Attach and detach a Cinder volume

Use Gophercloud's `volumeattach.Create` with the fixture server ID and an
`available` volume from the [Block Storage sandbox](volume.md).
`POST /servers/{server_id}/os-volume_attachments` returns `200` with the
attachment. Use `volumeattach.List` and `volumeattach.Get` to read the
collection and a particular attachment. At Nova v2.1 the attachment `id`
and the last segment of the item URL are the volume ID.

The fixture server must be `ACTIVE` or `SHUTOFF` to attach. See the
[Nova attachment state checks]. Each volume can have one attachment.
The device can be `/dev/vdb` through `/dev/vdz`. Omitting it picks the first
free device on the server. An unavailable volume, server reporting `ERROR`,
or occupied device returns `409`. Server and volume IDs are scoped to the
authenticated project.

The fixture completes attachment immediately in either supported state and
shows the corresponding `in-use` state in Cinder. A stopped server stays
`SHUTOFF` after attachment. `volumeattach.Delete` returns `202` and removes
the attachment immediately, making the volume `available` again. Existing
attachments remain readable and detachable while the server reports `ERROR`.
Attachment operations leave server detail faults pending. Reset clears the
attachments. See the [Nova volume attachment reference].

Server detail and detailed-list responses both include
`os-extended-volumes:volumes_attached`. At Nova v2.1 each item contains only
`id`, the Cinder volume ID. A server without attachments returns an empty
array. Gophercloud reads the field as `Server.AttachedVolumes`. See the
[Nova attachment representation].

## Inject and clear an instance fault

The server initially reports `ACTIVE`. Send a `PUT` with the SandStack admin
token to set its fault state.

```sh
server_id=d8ba25c2-8224-4e09-88ef-fab16c654770
fault_url="http://127.0.0.1:8080/_sandstack/compute/servers/${server_id}/fault"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$fault_url"
```

The control returns `204`. The server detail and the `ERROR` list now show
an instance fault with code `500`, a message, and its creation time. The
`ACTIVE` list is empty. Repeating the control preserves the current fault.
Send `DELETE` to the same URL to remove the fault and reveal the current
power state. Injecting and clearing an instance fault on a stopped server
shows `ERROR` and then `SHUTOFF`.

## Make a detail read stale

Start with an `ACTIVE` server. Send `PUT` to the `stale-detail` control to
capture its current detail, then inject the instance fault.

```sh
stale_url="${fault_url%/fault}/stale-detail"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$stale_url"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$fault_url"
```

Both controls return `204`. `GET /servers/{server_id}` keeps returning the
captured `ACTIVE` detail, including its original timestamp. The detailed
list shows `ERROR` and the current instance fault. Repeating `PUT` on
`stale-detail` keeps the original snapshot. Send `DELETE` to `stale-detail`
to resume live detail reads. The live fault can then be cleared separately.

The snapshot also retains the attached volume IDs. Capture an unattached
server and then attach a volume to exercise a stale read. The detail still
shows an empty array, while the detailed list and standalone attachment
endpoints show the new attachment. Clearing `stale-detail` exposes the
current attachment IDs in the detail response.

## Fail the next detail read

Use a one-shot outage to exercise a client's retry after HTTP `503`. Send
`PUT` to the `unavailable-detail` control for the listed server ID.

```sh
outage_url="${fault_url%/fault}/unavailable-detail"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$outage_url"
```

The next authorized detail request for that server receives `503` with a
`serviceUnavailable` error. A retry reads the same server state with the
same token. List requests and rejected detail requests leave the outage
pending. Repeating `PUT` while the outage is pending keeps one failure
scheduled. `DELETE` or a [sandbox reset](reset.md) clears it.

An active stale-detail snapshot is used on retry after the outage is
consumed.

Inspect [recent requests](requests.md) to check that the attempted detail
read received `503` before the same route returned `200`.

## Verification

Run `go test ./...` to check catalog discovery and the `ACTIVE` → `ERROR` →
`ACTIVE` sequence through Gophercloud. The stale-detail test also checks
that list and detail diverge while the snapshot is active and agree after
it is cleared. The transient-outage test verifies one failure followed by
recovery. The attachment tests check both Nova and Cinder through the SDK
and verify the status codes and response fields directly over HTTP. They
also check the attached volume IDs in server detail and detailed list
through attach, detach, and stale snapshot recovery.
The stopped-server attachment scenario checks that Cinder reports
`in-use` while the Nova server stays `SHUTOFF`.
The power cycle tests exercise stop and start through Gophercloud and
check the stopped server state, unchanged attachments, and wrong-state
conflicts. The [Nova stop reference] and [Nova start reference] describe
the request bodies and response status codes.
The reboot tests use the SDK to check state conflicts and hard reboot
recovery and HTTP to check accepted requests and empty responses.
The rename tests compare the Nova v2.1 update response with SDK detail and
list reads and check that a pending detail outage still fires.
The name filter test uses Gophercloud to find the renamed fixture and
checks the intersection with the status filter.
The metadata tests use the SDK to exercise replacement and clearing.
Raw HTTP checks cover the metadata envelope, invalid requests, and quota.
The flavor test follows the SDK server's flavor ID and checks the v2.1
swap and link fields over HTTP.
The raw HTTP checks use the [Compute list reference],
[Compute detail reference], [Compute fault guide], and [Nova fault response]
for the OpenStack response shapes.

[Compute list reference]: https://docs.openstack.org/api-ref/compute/#list-servers-detailed
[Gophercloud list options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L42-L55
[Compute detail reference]: https://docs.openstack.org/api-ref/compute/#show-server-details
[Nova flavor reference]: https://docs.openstack.org/api-ref/compute/#show-flavor-details
[Nova flavor response]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/views/flavors.py#L39-L68
[Gophercloud flavor lookup]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/flavors/requests.go#L191-L197
[Compute fault guide]: https://docs.openstack.org/api-guide/compute/faults.html
[Nova fault response]: https://github.com/openstack/nova/blob/master/nova/api/openstack/wsgi.py
[Nova stop reference]: https://docs.openstack.org/api-ref/compute/#stop-server-os-stop-action
[Nova start reference]: https://docs.openstack.org/api-ref/compute/#start-server-os-start-action
[Nova update reference]: https://docs.openstack.org/api-ref/compute/#update-server
[Nova update schema]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/schemas/servers.py#L350-L373
[Nova name validation]: https://github.com/openstack/nova/blob/master/nova/api/validation/parameter_types.py#L73-L87
[Gophercloud update options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L639-L681
[Nova update response]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/servers.py#L955-L994
[Nova metadata reference]: https://docs.openstack.org/api-ref/compute/#server-metadata-servers-metadata
[Nova metadata merge reference]: https://docs.openstack.org/api-ref/compute/#create-or-update-metadata-items
[Nova metadata schema]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/schemas/server_metadata.py#L42-L49
[Nova metadata state checks]: https://github.com/openstack/nova/blob/master/nova/compute/api.py#L5091-L5122
[Nova metadata quota]: https://docs.openstack.org/nova/latest/configuration/config.html#quota.metadata_items
[Gophercloud metadata operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L886-L930
[Gophercloud metadata merge operation]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L934-L949
[Nova metadata item operations]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/server_metadata.py#L73-L152
[Gophercloud metadata item operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L951-L1007
[Nova reboot reference]: https://docs.openstack.org/api-ref/compute/#reboot-server-reboot-action
[Nova reboot schema]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/schemas/servers.py#L495-L512
[Nova reboot state checks]: https://github.com/openstack/nova/blob/master/nova/compute/vm_states.py#L62-L67
[Gophercloud reboot options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L699-L750
[Gophercloud server actions]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L1379-L1391
[Nova volume attachment reference]: https://docs.openstack.org/api-ref/compute/#attach-a-volume-to-an-instance
[Nova attachment state checks]: https://github.com/openstack/nova/blob/master/nova/compute/api.py#L4786-L4796
[Nova attachment representation]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/views/servers.py#L709-L727
