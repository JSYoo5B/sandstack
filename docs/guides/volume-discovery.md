# Volume types and availability zones

[Block Storage sandbox](volume.md)

Use the project-scoped Block Storage endpoint to discover a volume type
and availability zone before [creating a volume](volume.md#create-and-provision-a-volume).

## Discover the default volume type

Use `volumetypes.Get(ctx, client, "default").Extract()` in Gophercloud
v2.15.0. It sends `GET /types/default` to the project endpoint and
returns the public `standard` fixture. Pass the returned ID to
`volumetypes.Get` for its details. Pass either the name or UUID in
`volumes.CreateOpts.VolumeType` to create a volume of that type. The
created volume reports the type name. An omitted type selects the same
fixture. An explicit empty string or `null` does the same. An unknown
type selector returns `404`. See the [Cinder default type
reference] and [Gophercloud volume type lookup].

Both reads return `200` with a `volume_type` object containing the
ID, name, description, public flag, and an empty `extra_specs` object.
The UUID detail includes Cinder's legacy public flag for project members.
An unknown ID returns `404`. See the [Cinder type detail reference] and
[Cinder volume type access policy]. The [Cinder type response tests]
cover the public fields and fields controlled by policy.

### List visible volume types

Use `volumetypes.List(client, nil).AllPages(ctx)` and
`volumetypes.ExtractVolumeTypes` in Gophercloud v2.15.0. The
`GET /types` response contains the public fixture in a `volume_types`
array with the same fields as UUID detail. In baseline Cinder v3.0,
project members see the types available to their project regardless of
the `is_public` option. `name` and `description` filters have no
effect on this member request. See the [Cinder type list controller]
and [Gophercloud volume type list options].

Set `Limit: 1` to exercise pagination. A full page includes a
`volume_type_links` entry with the fixture ID as the next marker. The
follow-up page is empty. Use the concrete type ID as `Marker`. An unknown
marker returns `404`. A raw `limit=0` yields an empty array. SandStack
returns `400` for invalid limits, repeated parameters, and unsupported
query keys. See the [Cinder collection links].

## Discover the storage availability zone

Use `availabilityzones.List(client).AllPages(ctx)` and
`availabilityzones.ExtractAvailabilityZones` from Gophercloud's
Block Storage v3 package. The project endpoint's
`GET /os-availability-zone` returns `200` with one
`availabilityZoneInfo` entry. Its `zoneName` is `nova` and its
`zoneState.available` is `true`. Pass that name in
`volumes.CreateOpts.AvailabilityZone` to select it explicitly and
read the same name from the created volume. See the [Cinder zone
reference], [Cinder zone response], and [Gophercloud zone operations].

## Verification

Run `go test ./...` to check type and zone discovery and volume creation
through Gophercloud. HTTP tests check the discovery response fields, list
pagination, and project scope.

[Cinder default type reference]: https://docs.openstack.org/api-ref/block-storage/v3/#show-default-volume-type
[Cinder type detail reference]: https://docs.openstack.org/api-ref/block-storage/v3/#show-volume-type-detail
[Cinder volume type access policy]: https://github.com/openstack/cinder/blob/master/cinder/policies/volume_access.py#L40-L63
[Cinder type response tests]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/tests/unit/api/v3/test_types_orig.py#L322-L371
[Cinder type list controller]: https://github.com/openstack/cinder/blob/master/cinder/api/v3/types.py#L69-L118
[Gophercloud volume type lookup]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/volumetypes/requests.go#L59-L66
[Gophercloud volume type list options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/blockstorage/v3/volumetypes/requests.go#L74-L130
[Cinder zone reference]: https://docs.openstack.org/api-ref/block-storage/v3/#list-availability-zone-information
[Cinder zone response]: https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/views/availability_zones.py#L20-L30
[Gophercloud zone operations]: https://github.com/gophercloud/gophercloud/tree/v2.15.0/openstack/blockstorage/v3/availabilityzones
[Cinder collection links]: https://github.com/openstack/cinder/blob/master/cinder/api/common.py#L270-L323
