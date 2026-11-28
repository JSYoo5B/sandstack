# Back up and restore a volume

[Block Storage sandbox](volume.md)

Use the project token and endpoint from the [Block Storage
sandbox](volume.md). Backups live under `/backups` on the same
`block-storage` endpoint, so Gophercloud's `NewBlockStorageV3` client
reaches them without another catalog entry. SandStack serves the Cinder
v3.0 behavior.

## Create a backup

Use `backups.Create` in Gophercloud v2.15.0 for `POST /backups`. The
`backup` object needs the source `volume_id`. It also accepts `name`,
`description`, `container`, `incremental`, `force`, and `snapshot_id`.
Each string may contain up to 255 characters or be `null`. SandStack trims
surrounding whitespace from the name and description. The `incremental`
and `force` flags accept a JSON boolean or a Cinder boolean string such as
`"true"` or `"no"`. Other fields return `400`, including `metadata` and
`availability_zone`, which require microversions 3.43 and 3.51. See the
[Cinder backup create schema], [Cinder backup create controller], and
[Gophercloud backup create options].

The response is `202` with a `backup` object that contains only `id`,
`name`, and `self` and `bookmark` links. The new backup is in `creating`
status and the source volume moves to `backing-up`. See the [Cinder backup
views] and [Cinder backup create request].

| Source | Result |
| --- | --- |
| `available` volume | Accepted |
| `in-use` volume with `force` set to true | Accepted. The attachment remains in place |
| `in-use` volume without `force`, or a volume in any other state | `400` |
| `snapshot_id` of an `available` snapshot of the same volume | Accepted. The snapshot moves to `backing-up` and the volume keeps its status |
| Snapshot of another volume, or a snapshot that is not `available` | `400` |
| Unknown volume or snapshot | `404` |

While a volume is `backing-up`, it cannot be deleted, attached, extended,
retyped, snapshotted, or backed up again. Nova returns `409` if you try
to detach it. The volume fault control also returns `409`.

Take the backup ID from the create response and drive the outcome with
the dedicated admin token.

```sh
backup_url="http://127.0.0.1:8080/_sandstack/volume/backups/${backup_id}"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$backup_url/complete-create"
```

`complete-create` moves the backup to `available`. `fail-create` moves it
to `error` with a `fail_reason`. Either outcome returns the volume to its
previous status, such as `available` or `in-use`, or returns the snapshot
to `available`. Each control returns `204` the first time and on repeats
of the same outcome. Requesting the other outcome afterwards returns
`409`. See the [Cinder backup completion] and [Cinder backup failure
handling].

### Create an incremental backup

Set `Incremental: true` to chain a backup to the most recent `available`
or `restoring` backup of the same volume, ranked by `data_timestamp` and
then `created_at`. A backup that is still `creating` or in `error` cannot
be a parent. Without a parent, the request returns `400`. For a snapshot
backup, the parent must have a `data_timestamp` earlier than the
snapshot's creation time. See the [Cinder incremental parent query].

The new backup reports `is_incremental: true`. After the incremental
backup completes, its parent reports `has_dependent_backups: true`.

## Read and list backups

Use `backups.Get` for `GET /backups/{backup_id}`. The detail contains
`id`, `status`, `size`, `object_count`, `availability_zone`, `container`,
`created_at`, `updated_at`, `name`, `description`, `fail_reason`,
`volume_id`, `links`, `is_incremental`, `has_dependent_backups`,
`snapshot_id`, and `data_timestamp`. See the [Cinder backup views].

- `size` is the source volume size in GiB.
- `availability_zone` is the backup service zone, `nova`.
- `container` keeps the requested value or stays `null`, since SandStack
  has no backup driver. For the same reason, `object_count` stays `0`.
- `data_timestamp` is the backup creation time, or the snapshot creation
  time for a snapshot backup.
- `updated_at` is set when Cinder saves the data timestamp of a volume
  backup and changes with each status transition.

Use `backups.List` for `GET /backups` to get summaries with `id`, `name`,
and `links`. Use `backups.ListDetail` for `GET /backups/detail` to get full
details. Both lists include only the token's project and accept the
`name`, `status`, and `volume_id` filters. Each filter must match exactly.
They also accept `limit`, `marker`, and `sort=id:asc`. Backups appear in
ascending ID order. A full page includes a `next` link in `backups_links`
that `AllPages` follows. An unknown marker returns `404`. Other query
keys and repeated keys return `400`. Use `ListOpts` for filters. Use
`ListDetailOpts` for paging. See the [Cinder backup list controller] and
[Gophercloud backup list options].

## Restore a backup

Use `backups.RestoreFromBackup` for `POST /backups/{backup_id}/restore`.
The `restore` object accepts an optional `volume_id` and `name`. The
backup must be `available`. The response is `202` with a `restore` object
that contains `backup_id`, `volume_id`, and `volume_name`. See the [Cinder
backup restore controller], [Cinder backup restore schema], and
[Gophercloud backup restore options].

Without `volume_id`, Cinder creates a volume of the backup size with the
default type and waits until it leaves `creating`. SandStack creates the
volume immediately. Its name is the requested `name` or
`restore_backup_<backup_id>`. Its description is
`auto-created_from_restore_from_backup`. With `volume_id`, the target must be
an `available` volume at least as large as the backup, and `name` is
ignored. An unknown volume returns `404`. A target in another state or
smaller than the backup returns `400`. See the [Cinder backup restore
request].

The backup moves to `restoring` and the target volume moves to
`restoring-backup`. Drive the outcome with the admin token.

```sh
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$backup_url/complete-restore"
```

`complete-restore` returns the backup and the target volume to
`available`. It also applies the source volume data that Cinder saves
with the backup. See the [Cinder restore completion] and [Cinder backup
metadata restore].

- If the source had a name and the target has no name or a name starting
  with `restore_backup_`, the target takes the source name and description.
- Nonempty source user metadata replaces the target metadata.
- Source image metadata replaces the target image metadata and makes the
  target bootable.
- When restoring to a volume other than the source, the target metadata
  receives `src_backup_id` with the backup ID. This also happens when the
  restored metadata names a different backup.

`fail-restore` moves the target volume to `error_restoring` and returns
the backup to `available`. A volume in `error_restoring` can be deleted.
Both controls return `204` for a pending restore and `409` otherwise.

## Delete a backup

Use `backups.Delete` for `DELETE /backups/{backup_id}`. An `available` or
`error` backup is removed and the response is `202` with an empty body. A
backup in another status returns `400`. A backup with any incremental
child returns `400` until that child is deleted. Deleting a completed child
clears its parent's `has_dependent_backups`. See the [Cinder backup
delete request].

Cinder allows deleting a source volume that has backups. Its backups
remain available for restore. [Reset the sandbox](reset.md) to discard
all backups and pending outcomes.

## Omissions

SandStack does not implement backup update, export and import,
`os-reset_status`, `os-force_delete`, backup metadata, availability zone
selection, quotas, or project and user attributes from later
microversions. Restores do not copy volume data, and encryption key
handling does not apply.

## Verification

The application tests cover the source state rules, the backing-up and
restoring transitions, parent selection for volume and snapshot backups,
dependent tracking, restore metadata, list filters, and reset. The
Gophercloud test creates full, incremental, and forced backups. It lists,
restores to a new and an existing volume, and deletes them. Raw HTTP tests
check request validation, the summary and detail fields, links, errors,
controls, paging, volume deletion with backups, and reset.

[Cinder backup create schema]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/schemas/backups.py#L25-L56
[Cinder backup create controller]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/backups.py#L147-L202
[Cinder backup create request]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/api.py#L218-L369
[Cinder backup views]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/views/backups.py#L37-L82
[Cinder backup completion]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/manager.py#L585-L620
[Cinder backup failure handling]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/manager.py#L557-L568
[Cinder incremental parent query]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6543-L6578
[Cinder backup list controller]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/backups.py#L76-L141
[Cinder backup restore controller]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/backups.py#L204-L237
[Cinder backup restore schema]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/schemas/backups.py#L80-L95
[Cinder backup restore request]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/api.py#L371-L451
[Cinder restore completion]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/manager.py#L645-L768
[Cinder backup metadata restore]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/driver.py#L175-L305
[Cinder backup delete request]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/api.py#L83-L118
[Gophercloud backup create options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/backups/requests.go#L19-L71
[Gophercloud backup list options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/backups/requests.go#L94-L197
[Gophercloud backup restore options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/backups/requests.go#L249-L277
