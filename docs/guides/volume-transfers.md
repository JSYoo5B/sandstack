# Transfer a volume

[Block Storage sandbox](volume.md)

Use the project token and endpoint from the [Block Storage
sandbox](volume.md). Transfers live under `/os-volume-transfer` on the
same `block-storage` endpoint, which is the path Gophercloud v2.15.0's
`transfers` package calls. SandStack serves the Cinder v3.0 behavior.
See the [Cinder transfer controller] and [Gophercloud transfer URLs].

Only the primary [Identity fixture](identity.md) project has Block Storage
access. [Additional fixture](identity.md#additional-fixtures) projects
receive `404` for transfers. In Cinder, the accepting project becomes the
owner of the volume. In SandStack, the only project that can accept a
transfer is the one that created it, so accept
leaves the owner unchanged. Cinder does not reject a transfer accepted
by the project that created it. See the [Cinder transfer acceptance].

## Create a transfer

Use `transfers.Create` with `transfers.CreateOpts{VolumeID: volumeID}`.
It sends `POST /os-volume-transfer` with a `transfer` object. The object
needs `volume_id` and accepts an optional `name`. The name may be `null`
or a string that has 1 to 255 characters once surrounding whitespace is
trimmed. SandStack stores the trimmed name. Any other field returns `400`,
including `no_snapshots`, which requires microversion 3.55. See the
[Cinder transfer schema] and [Cinder name format].

The response is `202` with a `transfer` object that contains `id`,
`created_at`, `name`, `volume_id`, `auth_key`, and `self` and `bookmark`
links. The `auth_key` is 16 lowercase hexadecimal characters. Only the
create response contains it. The volume moves to `awaiting-transfer` and
its `updated_at` changes. See the [Cinder transfer views] and [Cinder
transfer creation].

| Request | Result |
| --- | --- |
| `available` volume whose snapshots are all `available` | `202` |
| Volume in any other status, including a volume already in a transfer | `400` with `Invalid volume: status must be available` |
| Volume with a snapshot that is not `available` | `400` with `Invalid snapshot: snapshot: <snapshot_id> status must be available` |
| Unknown volume | `404` with `Volume <volume_id> could not be found.` |

While a volume is `awaiting-transfer`, it cannot be deleted, attached,
extended, retyped, cloned, snapshotted, backed up, or used as a restore
target. Cinder delete returns `400`. Nova attach returns `409`, as it
does for other volume states that cannot be attached. The volume fault
control also returns `409`. Name, description, metadata, and bootable
updates still apply, as Cinder blocks them only in maintenance. See the
[Cinder volume delete conditions].

## Read and list transfers

Use `transfers.Get` for `GET /os-volume-transfer/{transfer_id}`. The
detail contains `id`, `created_at`, `name`, `volume_id`, and `links`,
without `auth_key`. Use `transfers.List` for
`GET /os-volume-transfer/detail` to list the same details. `GET
/os-volume-transfer` lists summaries with `id`, `volume_id`, `name`, and
`links`. Both lists contain the token's project's transfers in ascending
`created_at` order, then by ID. An unknown transfer returns `404` with
`Transfer <transfer_id> could not be found.` See the [Cinder transfer
reads].

SandStack's lists take no query parameters and never include
`transfers_links`. A query string returns `400`. Pass `nil` or empty
`transfers.ListOpts` in Gophercloud.

## Accept a transfer

Use `transfers.Accept` with `transfers.AcceptOpts{AuthKey: authKey}`. It
sends `POST /os-volume-transfer/{transfer_id}/accept` with an `accept`
object. The object needs `auth_key` and accepts no other field. A JSON
integer is compared by its decimal text, as Cinder does. See the [Cinder
transfer schema].

| Request | Result |
| --- | --- |
| Matching `auth_key` | `202` with the transfer summary |
| Missing, `null`, or non-string, non-integer `auth_key`, or an extra field | `400` |
| Wrong `auth_key` | `400` with `Invalid auth key: Attempt to transfer <transfer_id> with invalid auth key.` |
| Unknown, cancelled, or accepted transfer | `404` with `Transfer <transfer_id> could not be found.` |

The `202` response contains a `transfer` object with `id`, `volume_id`,
`name`, and `links`. The volume returns to `available` and its
`updated_at` changes. The transfer is removed from reads and lists. A
rejected accept leaves the volume in `awaiting-transfer`. See the [Cinder
transfer acceptance] and [Cinder transfer record acceptance].

## Cancel a transfer

Use `transfers.Delete` for `DELETE /os-volume-transfer/{transfer_id}`.
The request body must be empty. The response is `202` with an empty body.
The volume returns to `available` and the transfer can no longer be read
or accepted. An unknown transfer returns `404`. See the [Cinder transfer
reads] and [Cinder transfer record deletion].

[Reset the sandbox](reset.md) to discard all transfers and restore the
fixture volume.

## Omissions

- Accept does not move the volume, its snapshots, or quota usage to
  another project or user, since only the primary fixture project has
  Block Storage access. A token for another project receives `404`.
- SandStack does not serve the `volume-transfers` resource, which Cinder
  documents for microversion 3.55 and later. Gophercloud v2.15.0 does
  not call it.
- `no_snapshots`, transfer history fields, encrypted volume transfers,
  `all_tenants`, list filters, sorting, and pagination are not
  implemented. Quota and consistency group checks do not apply.

## Verification

The application tests cover the volume and snapshot state rules, the
`awaiting-transfer` interactions with other volume operations, auth key
checks, acceptance, cancellation, and reset. The tests inject the auth key
generator. The Gophercloud test creates, reads, lists, accepts, and
cancels transfers with separate sessions for the creator and the
receiver. Raw HTTP tests check request validation, the create, detail,
and summary fields, links, the exact error messages, the `202` responses,
and reset.

[Cinder transfer controller]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/volume_transfer.py#L148-L164
[Cinder transfer reads]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/volume_transfer.py#L42-L145
[Cinder transfer schema]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/schemas/volume_transfer.py#L23-L58
[Cinder name format]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/validation/validators.py#L150-L163
[Cinder transfer views]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/views/transfers.py#L39-L98
[Cinder transfer creation]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/transfer/api.py#L131-L197
[Cinder transfer acceptance]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/transfer/api.py#L235-L351
[Cinder transfer record acceptance]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6996-L7061
[Cinder transfer record deletion]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6929-L6957
[Cinder volume delete conditions]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L476-L518
[Gophercloud transfer URLs]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/transfers/urls.go#L1-L23
