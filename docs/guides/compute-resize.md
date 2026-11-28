# Resize servers in the Compute sandbox

[Compute sandbox](compute.md)

Use Nova v2.1 `resize` to move a server to another flavor, then keep the
new flavor with `confirmResize` or return to the original with
`revertResize`. Start with a server from
[Boot and delete servers](compute-servers.md). The
[flavor fixture](compute.md#read-the-server-flavor) offers `m1.tiny`
(`1`) and `m1.small` (`2`).

## Resize a server

Use `servers.Resize` with `servers.ResizeOpts{FlavorRef: "2"}` in
Gophercloud v2.15.0. It sends `POST /servers/{server_id}/action` with
`{"resize":{"flavorRef":"2"}}`. `flavorRef` is required and can be a
non-empty string or an integer. Nova uses its string form as the flavor
ID without parsing links. `OS-DCF:diskConfig` is optional and accepts
`AUTO` or `MANUAL`. Other fields return `400`. A successful request
returns `202` with an empty body and records a `resize`
[server action](compute-actions.md). See the [Nova resize schema], [Nova
resize handler], and [Gophercloud resize].

Resize requires an `ACTIVE` or `SHUTOFF` server with no task in progress.
The new flavor replaces the server's flavor at once. The server then
reports `VERIFY_RESIZE` with `OS-EXT-STS:vm_state` `resized` and keeps
its power state: `1` for a server that was running and `4` for one that
was stopped. The `flavor` field shows the new flavor ID and bookmark. See
the [Nova resize API], [Nova resize completion], [Nova status mapping],
and [Nova flavor view].

### Rejected resize requests

Nova checks the request in this order:

| Condition | Status and message |
| --- | --- |
| Unknown server | `404` |
| Locked server | `409` `Instance {server_id} is locked` |
| Server not `ACTIVE` or `SHUTOFF` | `409` `Cannot 'resize' instance {server_id} while it is in vm_state {state}` |
| A task in progress, such as a held snapshot | `409` with `task_state {task}` |
| `AUTO` disk config on an image with `auto_disk_config` `disabled` | `400` `Requested image {image_id} has automatic disk resize disabled.` |
| Unknown flavor | `400` `Flavor {flavor_ref} could not be found.` |
| The current flavor | `400` `When resizing, instances must change flavor!` |
| Flavor RAM below the image `min_ram` | `400` `Flavor's memory is too small for requested image.` |
| Image-backed server whose image `min_disk` exceeds the flavor's root disk | `400` with both sizes in bytes |

`{state}` is `building`, `error`, `paused`, `suspended`, or `resized`.
Nova compares against the `min_ram` and `min_disk` it recorded when it
built the server. `min_disk` is the larger of the image's value and the
boot flavor's root disk, so a server booted with `m1.small` cannot be
resized to `m1.tiny`. A server that boots from a volume skips the disk
check. A rejected request leaves the server unchanged and records no
action. See the [Nova state check], [Nova lock check], [Nova state
conflict], [Nova flavor image check], and [Nova system metadata mapping].

## Confirm or revert

Use `servers.ConfirmResize` to keep the new flavor. It sends
`{"confirmResize":null}`, and Nova returns `204` with an empty body. The
server returns to `ACTIVE`, or to `SHUTOFF` if it was stopped before the
resize, and keeps the new flavor. See the [Nova confirm handler] and
[Nova confirm completion].

Use `servers.RevertResize` to restore the original flavor. It sends
`{"revertResize":null}`, and Nova returns `202` with an empty body. The
server returns to its original flavor and to `ACTIVE` or `SHUTOFF` as it
was before the resize. See the [Nova revert handler] and [Nova revert
completion].

Both actions accept any value for their key and record `confirmResize`
or `revertResize`. A locked server returns `409` `Instance {server_id} is
locked`. A server that is not in `VERIFY_RESIZE` returns `409` `Cannot
'confirmResize' instance {server_id} while it is in vm_state {state}`, or
the `revertResize` equivalent. See the [Nova confirm and revert checks].

## Wait for the resize

SandStack completes a resize, confirm, or revert before it answers, as it
does for [builds](compute-servers.md#wait-for-the-server). Wait with
`servers.WaitForStatus` for `VERIFY_RESIZE`, then for `ACTIVE` or
`SHUTOFF` after confirming or reverting. Nova moves through `RESIZE` and
`REVERT_RESIZE` while its compute service works. SandStack does not
report these transient statuses. See [Gophercloud WaitForStatus].

## Actions during VERIFY_RESIZE

A server in `VERIFY_RESIZE` accepts volume attachment and detachment,
lock and unlock, rename, and deletion. `os-stop`, `os-start`, soft and
hard `reboot`, `pause`, `suspend`, `createImage`, `rebuild`, another
`resize`, and metadata writes return `409`. Use `?status=VERIFY_RESIZE`
to find resized servers in lists. See the [Nova stop state check], [Nova reboot
state checks], [Nova snapshot state check], and [Nova attachment state
checks].

[Reset the sandbox](reset.md) removes created servers, which also ends
their pending resizes.

## Omissions

SandStack does not model migrations or the scheduler, so `RESIZE` and
`REVERT_RESIZE`, cold migration without a flavor, the automatic confirm
window, quotas, resizes to zero-disk or disabled flavors, flavor extra
specs, and the compute service check that rejects shrinking a local disk
are absent. Nova sets `auto_disk_config` from the resize request, but
v2.1 server detail does not report `OS-DCF:diskConfig`.

## Verification

Run `go test ./internal/api/server -run 'Resize|Flavor' -count=1` and
`go test ./internal/app/compute -run 'TestServersSuite/.*Resiz' -count=1`.
The Gophercloud tests resize a running server, wait for `VERIFY_RESIZE`,
check the new flavor, power state, and actions, and confirm. They also
resize a stopped server, check the `409` for `os-stop`, and revert to
`SHUTOFF` with the original flavor. Raw HTTP checks the schema `400`
cases, the flavor `400` messages, the `409` messages for locked and
wrong-state servers, the `202` and `204` empty responses, the flavor
link, and the action record. The application tests check the disk,
memory, and disk config checks, the volume-backed exception, and the
actions accepted during `VERIFY_RESIZE` against the pinned Nova source.

[Nova resize schema]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L467-L482
[Nova resize handler]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1149-L1216
[Gophercloud resize]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L819-L882
[Nova resize API]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4304-L4513
[Nova resize completion]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L6520-L6648
[Nova status mapping]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L76-L82
[Nova flavor view]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/views/servers.py#L665-L683
[Nova state check]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L142-L180
[Nova lock check]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L247-L253
[Nova state conflict]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L330-L346
[Nova flavor image check]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L781-L898
[Nova system metadata mapping]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/utils.py#L536-L561
[Nova confirm handler]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1081-L1103
[Nova confirm completion]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L5112-L5130
[Nova revert handler]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1105-L1127
[Nova revert completion]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L5694-L5790
[Nova confirm and revert checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4019-L4180
[Gophercloud WaitForStatus]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/util.go#L11-L30
[Nova stop state check]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2882-L2885
[Nova reboot state checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3655-L3686
[Nova snapshot state check]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3454-L3460
[Nova attachment state checks]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5225-L5230
