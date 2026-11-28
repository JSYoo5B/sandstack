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

### Select a flavor by minimum capacity

Use `flavors.ListDetail` with `flavors.ListOpts{MinRAM: 512, MinDisk: 1}`
to select flavors through `GET /flavors/detail`. RAM is measured in MiB
and disk in GiB. Each supplied minimum is inclusive. A flavor must meet
both when they are supplied together. The list uses the same fields as
the detail response. When the fixture falls below a minimum, the response
is `200` with an empty `flavors` array.

SandStack accepts one `minRam` and one `minDisk` integer query value.
Other query keys, repeated keys, and malformed values return `400`.
The single-fixture list returns at most one row in one page. See the
[Nova detailed flavor list] and [Gophercloud flavor list options].

## List server summaries

Use `servers.ListSimple` in Gophercloud v2.15.0 for `GET /servers`.
Nova v2.1 returns a `servers` array with each server's ID, name, and
links. The links point to the same resources as the detailed list.
Use the returned ID with `servers.Get` to read the full server.
A list with no matches returns `{"servers":[]}`. See the [Nova server
summary schema] and [Gophercloud basic list].

## Filter the detailed server list

The same filters apply to basic and detailed lists.

The detailed list supports a single `status` filter. Use `?status=ACTIVE`,
`?status=ERROR`, `?status=SHUTOFF`, `?status=PAUSED`, or
`?status=SUSPENDED` to select a state. Values match without regard to
case. An empty, nonmatching, or unknown status produces an empty `servers`
array.

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

See [Read and replace server metadata](compute-metadata.md#read-and-replace-server-metadata).

### Merge server metadata

See [Merge server metadata](compute-metadata.md#merge-server-metadata).

### Use an individual metadata item

See [Use an individual metadata item](compute-metadata.md#use-an-individual-metadata-item).

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
`PAUSED`, `SUSPENDED`, or `ERROR`. A soft reboot in any of the latter
states returns `409` and preserves the state. A successful hard reboot can
recover the injected instance fault. SandStack completes the reboot before
responding, so live detail and detailed list show `ACTIVE` immediately.
Nova runs reboots asynchronously. Existing attachments, stale detail
snapshots, and scheduled detail failures remain in place. See the [Nova
reboot state checks].

## Pause and unpause a server

Use `servers.Pause` and `servers.Unpause` with the fixture server ID in
Gophercloud v2.15.0. They send `POST /servers/{server_id}/action` with
`{"pause":null}` or `{"unpause":null}`. Nova v2.1 accepts any value for
the action key. A second action key or an extra field returns `400`. An
accepted action returns `202` with an empty body. See the [Nova pause
reference], [Nova pause schema], and [Gophercloud pause operations].

Pause requires `ACTIVE` and unpause requires `PAUSED`. Any other state,
including an injected `ERROR`, returns `409`. A paused server reports
`OS-EXT-STS:vm_state` as `paused` and `OS-EXT-STS:power_state` as `3`.
Stop, start, and soft reboot return `409` while it is paused. Hard reboot
returns it to `ACTIVE`. See the [Nova pause state checks], [Nova status
mapping], and [Nova power states].

SandStack completes each accepted transition before it responds, so live
detail and the detailed list show the new state immediately. Nova pauses
asynchronously. A paused server accepts volume attachment and metadata
writes. Existing attachments, stale detail snapshots, and scheduled detail
failures remain in place.

## Suspend and resume a server

Use `servers.Suspend` and `servers.Resume` with the fixture server ID in
Gophercloud v2.15.0. They send `POST /servers/{server_id}/action` with
`{"suspend":null}` or `{"resume":null}`. The body rules and responses
match pause: any value for the action key, `400` for a second key or an
extra field, and `202` with an empty body. See the [Nova suspend
reference], [Nova resume reference], [Nova suspend schema], and
[Gophercloud suspend operations].

Suspend requires `ACTIVE` and resume requires `SUSPENDED`. Any other state,
including `PAUSED` or an injected `ERROR`, returns `409`. A suspended
server reports `OS-EXT-STS:vm_state` as `suspended`. Nova stores the
power state reported by the hypervisor driver. SandStack reports `4`,
the shutdown value the libvirt driver reports after it saves the guest
memory state. Stop, start, pause, and soft reboot return `409` while the
server is suspended. Hard reboot returns it to `ACTIVE`. See the [Nova
suspend state checks], [Nova suspend completion], [libvirt suspend], and
[libvirt power states].

SandStack completes each accepted transition before it responds. A
suspended server accepts metadata writes but rejects volume attachment and
detachment with `409`. Detaching a volume that is not attached returns
`404` before the state check. See the [Nova detach state checks] and
[Nova detach request order]. Existing attachments, stale detail
snapshots, and scheduled detail failures remain in place.

## Attach and detach a Cinder volume

Use Gophercloud's `volumeattach.Create` with the fixture server ID and an
`available` volume from the [Block Storage sandbox](volume.md).
`POST /servers/{server_id}/os-volume_attachments` returns `200` with the
attachment. Use `volumeattach.List` and `volumeattach.Get` to read the
collection and a particular attachment. At Nova v2.1 the attachment `id`
and the last segment of the item URL are the volume ID.

The fixture server must be `ACTIVE`, `PAUSED`, or `SHUTOFF` to attach. See
the [Nova attachment state checks]. Each volume can have one attachment.
The device can be `/dev/vdb` through `/dev/vdz`. Omitting it picks the
first free device on the server. An unavailable volume, server reporting
`ERROR`, or occupied device returns `409`. Server and volume IDs are
scoped to the authenticated project.

The fixture completes attachment immediately in either supported state and
shows the corresponding `in-use` state in Cinder. A stopped server stays
`SHUTOFF` after attachment. `volumeattach.Delete` returns `202` and removes
the attachment immediately, making the volume `available` again. Existing
attachments remain readable and detachable while the server reports `ERROR`.
Detaching from a `SUSPENDED` server returns `409`.
Attachment operations leave server detail faults pending. Reset clears the
attachments. See the [Nova volume attachment reference].

Server detail and detailed-list responses both include
`os-extended-volumes:volumes_attached`. At Nova v2.1 each item contains only
`id`, the Cinder volume ID. A server without attachments returns an empty
array. Gophercloud reads the field as `Server.AttachedVolumes`. See the
[Nova attachment representation].

## Inject and clear an instance fault

See [Inject and clear an instance fault](compute-faults.md#inject-and-clear-an-instance-fault).

## Make a detail read stale

See [Make a detail read stale](compute-faults.md#make-a-detail-read-stale).

## Fail the next detail read

See [Fail the next detail read](compute-faults.md#fail-the-next-detail-read).

## Verification

Run `go test ./...` to check catalog discovery and fixture operations
through Gophercloud. The attachment tests verify the same volume in
Nova and Cinder and check the HTTP response fields.
The stopped-server attachment scenario checks that Cinder reports
`in-use` while the Nova server stays `SHUTOFF`.
The power cycle tests exercise stop and start through Gophercloud and
check the stopped server state, unchanged attachments, and wrong-state
conflicts. The [Nova stop reference] and [Nova start reference] describe
the request bodies and response status codes.
The reboot tests use the SDK to check state conflicts and hard reboot
recovery and HTTP to check accepted requests and empty responses.
The pause tests use the SDK to check the paused state through detail and
the `PAUSED` filter, conflicts with other power actions, attachment while
paused, and hard reboot recovery. Raw HTTP checks the action schema,
`202` empty responses, `409` conflicts, and the extended state fields
against the pinned Nova source.
The suspend tests use the SDK to check the suspended state through detail
and the `SUSPENDED` filter, conflicts with power and detach actions, and
hard reboot recovery. Raw HTTP also checks the attachment conflict and the
`404` for detaching a volume that is not attached.
The rename tests compare the Nova v2.1 update response with SDK detail and
list reads and check that a pending detail outage still fires.
The name filter test uses Gophercloud to find the renamed fixture and
checks the intersection with the status filter.
The flavor test follows the SDK server's flavor ID and checks the v2.1
swap and link fields over HTTP.
The raw HTTP checks follow the [Compute list reference] and
[Compute detail reference] for the OpenStack response shapes.

[Compute list reference]: https://docs.openstack.org/api-ref/compute/#list-servers-detailed
[Nova server summary schema]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L768-L788
[Gophercloud basic list]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/testing/requests_test.go#L45-L60
[Gophercloud list options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L42-L55
[Compute detail reference]: https://docs.openstack.org/api-ref/compute/#show-server-details
[Nova flavor reference]: https://docs.openstack.org/api-ref/compute/#show-flavor-details
[Nova flavor response]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/views/flavors.py#L39-L68
[Gophercloud flavor lookup]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/flavors/requests.go#L191-L197
[Nova detailed flavor list]: https://docs.openstack.org/api-ref/compute/#list-flavors-with-details
[Gophercloud flavor list options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/flavors/requests.go#L43-L94
[Nova stop reference]: https://docs.openstack.org/api-ref/compute/#stop-server-os-stop-action
[Nova start reference]: https://docs.openstack.org/api-ref/compute/#start-server-os-start-action
[Nova update reference]: https://docs.openstack.org/api-ref/compute/#update-server
[Nova update schema]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/schemas/servers.py#L350-L373
[Nova name validation]: https://github.com/openstack/nova/blob/master/nova/api/validation/parameter_types.py#L73-L87
[Gophercloud update options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L639-L681
[Nova update response]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/servers.py#L955-L994
[Nova reboot reference]: https://docs.openstack.org/api-ref/compute/#reboot-server-reboot-action
[Nova reboot schema]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/schemas/servers.py#L495-L512
[Nova reboot state checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/vm_states.py#L69-L73
[Gophercloud reboot options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L699-L750
[Nova pause reference]: https://docs.openstack.org/api-ref/compute/#pause-server-pause-action
[Nova pause schema]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/pause_server.py#L13-L31
[Nova pause state checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4851-L4867
[Nova status mapping]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L83-L89
[Nova power states]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/objects/fields.py#L1112-L1131
[Gophercloud pause operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L1235-L1247
[Nova suspend reference]: https://docs.openstack.org/api-ref/compute/#suspend-server-suspend-action
[Nova resume reference]: https://docs.openstack.org/api-ref/compute/#resume-suspended-server-resume-action
[Nova suspend schema]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/suspend_server.py#L13-L31
[Nova suspend state checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4880-L4908
[Nova suspend completion]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L7138-L7162
[libvirt suspend]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/virt/libvirt/driver.py#L4505-L4517
[libvirt power states]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/virt/libvirt/guest.py#L67-L83
[Nova detach state checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5328-L5333
[Nova detach request order]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/volume_attachments.py#L394-L440
[Gophercloud suspend operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L1393-L1405
[Gophercloud server actions]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L1379-L1391
[Nova volume attachment reference]: https://docs.openstack.org/api-ref/compute/#attach-a-volume-to-an-instance
[Nova attachment state checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5225-L5230
[Nova attachment representation]: https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/views/servers.py#L709-L727
