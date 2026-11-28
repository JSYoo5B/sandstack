# Change a volume type

[Block Storage sandbox](volume.md)

Use the project token and endpoint from the [Block Storage
sandbox](volume.md) to move a volume between the fixture's two public
volume types, `standard` and `performance`. Find both through
[volume type discovery](volume-discovery.md#list-visible-volume-types).

## Retype a volume

Use `volumes.ChangeType` in Gophercloud v2.15.0 with
`volumes.ChangeTypeOpts{NewType: "performance"}`. It sends
`POST /volumes/{volume_id}/action` with an `os-retype` object. Pass
the type name or ID in `new_type`. The optional `migration_policy` is
`never` or `on-demand`. The request returns `202` with an empty body.
See the [Cinder retype action], [Cinder retype schema], and [Gophercloud
retype options].

The volume must be `available` or `in-use`. Volume detail and the
detailed list then report the new `volume_type` and a new `updated_at`.
The status, size, attachments, and metadata stay the same. An attached
volume remains `in-use`. See the [Cinder retype request].

Both fixture types have empty extra specs and no QoS or encryption, so
Cinder retypes the volume without migration. SandStack applies the new
type before it responds, and the transient `retyping` status is not
visible. Either migration policy gives the same result, and snapshots do
not block the retype. A volume created later from one of the volume's
snapshots without an explicit type receives the new type. See the [Cinder
retype completion] and [Cinder type inheritance].

## Errors

| Request | Status |
| --- | --- |
| Missing `os-retype` object or `new_type`, non-string `new_type`, unknown or `null` `migration_policy`, or an extra field | `400` |
| Volume in a state other than `available` or `in-use` | `400` |
| `new_type` names the volume's current type | `400` |
| Unknown type name or ID, including the `default` lookup alias | `404` |
| Unknown volume | `404` |

A rejected request leaves the volume unchanged. See the [Cinder retype
tests]. A [sandbox reset](reset.md) restores the fixture volume's
`standard` type.

## Verification

The application tests cover state and type validation, retype with an
attachment and a snapshot, type inheritance, and reset. The Gophercloud
test retypes the fixture volume while available and attached, and creates
a volume from an earlier snapshot. Raw HTTP tests check request
validation, error statuses, the empty `202` response, and reset.

[Cinder retype action]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/volume_actions.py#L307-L317
[Cinder retype schema]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/schemas/volume_actions.py#L125-L142
[Cinder retype request]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L1883-L2004
[Cinder retype completion]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/manager.py#L3071-L3225
[Cinder type inheritance]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/flows/api/create_volume.py#L397-L430
[Cinder retype tests]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/tests/unit/api/contrib/test_volume_actions.py#L513-L607
[Gophercloud retype options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/volumes/requests.go#L658-L704
