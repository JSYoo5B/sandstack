# Volume sources in the Block Storage sandbox

[Block Storage sandbox](volume.md)

Use the project-scoped Block Storage endpoint to create a volume from a
Glance image, a Cinder snapshot, or an existing Cinder volume.

## Create a volume from a Glance image

Use a private `active` image from the same project. Pass its UUID as
`ImageID` in Gophercloud's `volumes.CreateOpts`, and specify the size
in GiB. Gophercloud sends `imageRef`. A raw request can also use
`image_id`. The size must cover both the image's stored bytes, rounded
up to GiB, and its minimum disk size. A missing or inactive image, an
undersized volume, or a request that also names a snapshot returns
`400`. See the [Cinder image source validation] and [Gophercloud volume
create options].

The `202` response shows the volume in `creating` state with
`bootable: "false"`. SandStack captures the image description on
acceptance. Use the [provisioning control](volume.md#create-and-provision-a-volume)
to set `available` and publish `bootable: "true"` and
`volume_image_metadata` together. The detail and detailed list expose
the source ID, name, formats, checksum, size, and capacity minimums
as strings. User volume metadata continues to use its own `metadata`
object. Provisioning failure leaves the volume unbootable and omits the
source metadata. See the [Cinder image metadata capture] and
[Cinder volume image metadata projection].

To prepare a source with your own bytes, [upload a Glance
image](image-data.md#upload-a-queued-image-file).

### Read published volume image metadata directly

Send `POST /volumes/{volume_id}/action` with
`{"os-show_image_metadata":{}}`. This Cinder v3.0 action returns `200`
with a `metadata` object when image metadata is present. A provisioned
image-backed volume returns the same image metadata as its detail
response, even if its bootable flag was later cleared. An unknown volume
or a volume without published image metadata returns `404`. This
includes blank volumes and pending image-backed volumes. The action
leaves scheduled detail failures pending. See the [Cinder show image
metadata action] and [Cinder image metadata lookup].

Use Gophercloud v2.15.0's `ServiceClient.Post` with the action URL and
`200` as the accepted response code.

## Create a volume from a snapshot

Use the [snapshot guide](volume-snapshots.md) to create an available snapshot.
Pass an `available` snapshot from the same project as `SnapshotID` in
Gophercloud's `volumes.CreateOpts`. Leave `Size` unset to inherit the
snapshot's size. A raw request can omit `size` or set it to JSON `null`.
An explicit size must be an integer in GiB at least as large as the
snapshot. A snapshot absent from the project returns `404`. A snapshot in
`creating` or `error` status, or an undersized request, returns `400`.

The `202` response contains a new volume in `creating` status. The create
response, volume detail, and detailed list expose the new volume's size
and the originating `snapshot_id`. Use the
[volume provisioning controls](volume.md#create-and-provision-a-volume) to complete
the new volume or simulate a provisioning failure. The new volume keeps
its `snapshot_id` in either outcome. This scenario models provisioning and
source metadata.

## Clone an existing volume

Pass an `available` or `in-use` volume from the same project as
`SourceVolID` in `volumes.CreateOpts`. Gophercloud sends `source_volid`.
Leave `Size` unset, or send raw `null`, to inherit the source size.
An explicit size must be at least as large as the source. A missing
source returns `404`. An unsupported source state, an undersized
request, or combining source selectors returns `400`. See the [Cinder
clone validation] and [Gophercloud volume create options].

The `202` response exposes the source ID and inherited bootable flag
while the clone is `creating`. Complete it through the
[provisioning control](volume.md#create-and-provision-a-volume). The clone uses
the user metadata from its own request. Glance metadata from a bootable
source is captured on acceptance and appears on clone detail and list
after successful provisioning. Source attachments and scheduled detail
failures stay with the source volume. The clone retains its source ID
after source deletion. See the [Cinder clone record] and [Cinder clone
metadata copy].

## Verification

Run `go test ./...` to exercise source selection and provisioning through
Gophercloud. The image tests check capacity, bootable state, and metadata
after provisioning. The snapshot and clone tests check size inheritance,
source validation, and transitions through provisioning. The HTTP tests
check the [create volume reference]. See [Cinder snapshot lookup],
[Cinder size default], and [Cinder volume creation flow] for snapshot
source and size validation.

[create volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#create-a-volume
[Cinder clone validation]: https://github.com/openstack/cinder/blob/10.0.0/cinder/volume/flows/api/create_volume.py#L128-L196
[Cinder clone record]: https://github.com/openstack/cinder/blob/10.0.0/cinder/volume/flows/api/create_volume.py#L521-L547
[Cinder clone metadata copy]: https://github.com/openstack/cinder/blob/10.0.0/cinder/volume/flows/manager/create_volume.py#L483-L501
[Cinder image source validation]: https://github.com/openstack/cinder/blob/10.0.0/cinder/volume/flows/api/create_volume.py#L194-L233
[Cinder image metadata capture]: https://github.com/openstack/cinder/blob/10.0.0/cinder/volume/flows/manager/create_volume.py#L561-L595
[Cinder volume image metadata projection]: https://github.com/openstack/cinder/blob/master/cinder/api/contrib/volume_image_metadata.py#L43-L79
[Cinder show image metadata action]: https://github.com/openstack/cinder/blob/master/cinder/api/contrib/volume_image_metadata.py#L103-L117
[Cinder image metadata lookup]: https://github.com/openstack/cinder/blob/10.0.0/cinder/db/sqlalchemy/api.py#L4341-L4355
[Gophercloud volume create options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/volumes/requests.go#L105-L164
[Cinder snapshot lookup]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L369-L375
[Cinder size default]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L408-L414
[Cinder volume creation flow]: https://github.com/openstack/cinder/blob/master/cinder/volume/flows/api/create_volume.py#L127-L205
