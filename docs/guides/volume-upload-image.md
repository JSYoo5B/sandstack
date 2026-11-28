# Upload a volume to an image

[Block Storage sandbox](volume.md)

Use the project token and endpoint from the [Block Storage
sandbox](volume.md) to upload an `available` volume into a new Glance
image. Read the image through the [Image sandbox](image.md).

## Upload a volume

Use `volumes.UploadImage` in Gophercloud v2.15.0 with
`volumes.UploadImageOpts`. It sends `POST /volumes/{volume_id}/action` with
an `os-volume_upload_image` object. At microversion 3.0 the object accepts:

- `image_name`, required, a string of 1 to 255 characters.
- `force`, a JSON boolean or one of Cinder's boolean strings such as
  `"true"` or `"off"`.
- `disk_format`, one of `raw`, `vmdk`, `vdi`, `qcow2`, `vhd`, `vhdx`, or
  `ploop`. The default is `raw`.
- `container_format`, a string of up to 255 characters or `null`. The
  default is `bare`.

`visibility` and `protected` belong to microversion 3.1. SandStack serves
3.0, where Cinder's schema rejects them and any other field with `400`.
Leave `Visibility` and `Protected` unset in `UploadImageOpts`. See the
[Cinder upload action], [Cinder upload schema], and [Gophercloud upload
options].

The response is `202` with an `os-volume_upload_image` object:

| Field | Value |
| --- | --- |
| `id`, `size`, `display_description` | The volume's ID, size in GiB, and description |
| `updated_at` | The volume's update timestamp before the upload |
| `status` | `uploading` |
| `volume_type` | The volume type object |
| `image_id`, `image_name` | The new image's ID and name |
| `container_format`, `disk_format` | The image's formats |
| `protected`, `visibility` | `false` and `shared`, as Glance returns them |

Cinder adds `protected` and `visibility` from the image that Glance
returns, so they appear at 3.0 as well. The `volume_type` object has `id`,
`name`, `description`, `is_public`, `extra_specs`, `qos_specs_id`
(`null`), `deleted` (`false`), and `deleted_at` (`null`). Gophercloud
v2.15.0 reads the type description from `display_description`, so
`VolumeImage.VolumeType.Description` stays empty. See [Cinder copy volume
to image], [Cinder Glance translation], and the [Cinder volume type
object].

### Volume state

Cinder's `enable_force_upload` option defaults to `false`, and Cinder then
ignores `force`. The volume must be `available` even with `force: true`.
An `in-use` or other volume returns `400` with `Invalid volume: Volume
{volume_id} status must be available`. See the [Cinder force upload
option] and [Cinder copy volume to image].

Cinder moves the volume to `uploading` and its volume manager restores
`available` after the upload. SandStack completes the upload before it
answers, as it does for [image-backed server
snapshots](compute-snapshots.md#wait-for-the-snapshot). The next volume
read reports `available` with a new `updated_at`, and the transient
`uploading` status is not visible. SandStack has no hold control for
uploads. See the [Cinder upload manager].

## The Glance image

Cinder creates a `queued` image owned by the volume's project with the
requested name and formats. At microversion 3.0 Cinder sends no
visibility, so Glance makes the image `shared` and not protected, with
`min_disk` and `min_ram` `0`. The image has no members until its owner
[adds them](image-members.md). See the [Glance image defaults]. Cinder copies the volume's
image metadata into the image properties except the Glance core
properties and reserved namespaces. SandStack records only core keys as
volume image metadata (`image_id`, `image_name`, `checksum`,
`container_format`, `disk_format`, `size`, `min_disk`, and `min_ram`),
so the image has no additional properties. See [Cinder image metadata
merge], [Cinder core properties], and [Cinder reserved namespaces].

The image is `active` when the response arrives. Its bytes follow the
[server snapshot](compute-snapshots.md#image-backed-servers) model: a
volume created from a Glance image, or from a snapshot or clone of such a
volume, uploads the source image's current bytes, size, and checksum. A
blank volume, or one whose source image was deleted, uploads empty data
with size `0` and the MD5 of empty data. Cinder uploads the volume's
contents, converted to `disk_format`. SandStack does not store volume
contents, so it does not model that conversion or the volume's full
size. See [Cinder upload volume].

### Formats that Glance rejects

Cinder sends `disk_format` and `container_format` to Glance, which checks
them against its configured lists. Glance 12.0.0 defaults to the
container formats `ami`, `ari`, `aki`, `bare`, `ovf`, `ova`, and `docker`
and the disk formats `ami`, `ari`, `aki`, `vhd`, `vmdk`, `raw`, `qcow2`,
`vdi`, and `iso`. Cinder accepts `vhdx` and `ploop`, but Glance 12.0.0
rejects them, as it rejects an empty or unknown `container_format`. Cinder
restores the volume status and returns `400`. SandStack creates no image
and leaves the volume unchanged. See the [Glance format defaults] and the
[Glance format schema].

With `container_format` `null`, Glance creates the image without a
container format. The upload then fails because Glance requires both
formats before it accepts data, and Cinder's volume manager deletes the
queued image. SandStack answers `202` with `container_format` `null`,
deletes the image, and leaves the volume `available`. See the [Glance
format requirement] and [Cinder image cleanup].

## Errors

| Request | Status |
| --- | --- |
| Missing or `null` action object, missing, empty, or non-string `image_name`, invalid `force`, unknown or `null` `disk_format`, a `container_format` longer than 255 characters, `visibility`, `protected`, or another field | `400` |
| `disk_format` or `container_format` that Glance 12.0.0 rejects | `400` |
| Volume not `available` | `400` |
| Unknown volume or another project's volume | `404` |

The checks run in this order: request schema, volume lookup, volume
state, then the Glance formats. A rejected request creates no image and
leaves the volume unchanged. Cinder applies no quota to uploads, and the
Glance 12.0.0 storage quota is unlimited by default, so SandStack has no
quota error for this action.

A [sandbox reset](reset.md) removes uploaded images.

## Omissions

SandStack does not model microversion 3.1 `visibility` and `protected`,
`enable_force_upload`, encrypted volumes, the `compressed` container
format of later Glance releases, the Glance `saving` status, Cinder user
messages for failed uploads, or the volume type's `created_at` and
`updated_at`, which the fixture types do not have. The rejection messages
for Glance formats are not Glance's wording. These values have not been
checked against a reference deployment.

## Verification

Run `go test ./internal/app/volume -run UploadImage -count=1` and
`go test ./internal/api/server -run 'UploadImage|UploadsCinder' -count=1`.
The application tests check the active image, data copied from an image
volume, the deleted image without a container format, the state checks
with and without `force`, and the Glance format lists. The Gophercloud
test uploads a volume created from the fixture image, reads the image
and its bytes through Glance, uploads a blank volume, and checks the
rejected forced upload of an attached volume. Raw HTTP checks the
request validation, the `400` and `404` cases, the `202` response
fields and `volume_type` object, the deleted image after a `null`
container format, and reset, all against the pinned Cinder and Glance
sources. `go test ./internal/api/server -run CinderCrossProjectWire
-count=1` uploads a volume owned by an [additional
fixture](identity.md#additional-fixtures) project over raw HTTP and reads
the `shared` image that project owns.

[Cinder upload action]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/volume_actions.py#L199-L269
[Cinder upload schema]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/schemas/volume_actions.py#L162-L192
[Gophercloud upload options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/volumes/requests.go#L538-L585
[Cinder copy volume to image]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L1483-L1552
[Cinder Glance translation]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/image/glance.py#L525-L638
[Cinder volume type object]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/objects/volume_type.py#L43-L53
[Cinder force upload option]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L74-L80
[Cinder upload manager]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/manager.py#L1752-L1803
[Cinder image metadata merge]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L1457-L1481
[Cinder core properties]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/image/glance.py#L81-L87
[Cinder reserved namespaces]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/image/image_utils.py#L1466-L1499
[Cinder upload volume]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/image/image_utils.py#L1161-L1239
[Glance image defaults]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/domain/__init__.py#L71-L94
[Glance format defaults]: https://github.com/openstack/glance/blob/12.0.0/glance/common/config.py#L44-L56
[Glance format schema]: https://github.com/openstack/glance/blob/12.0.0/glance/api/v2/images.py#L882-L891
[Glance format requirement]: https://github.com/openstack/glance/blob/12.0.0/glance/domain/__init__.py#L151-L163
[Cinder image cleanup]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/manager.py#L1784-L1819
