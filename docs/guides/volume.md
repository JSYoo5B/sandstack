# Block Storage sandbox

Use the Block Storage sandbox to test a client that discovers a volume and
handles a storage error. Authenticate with the [Identity fixture](identity.md).
Gophercloud's `NewBlockStorageV3` selects the public `block-storage` endpoint
in `RegionOne`. The catalog endpoint is `/volume/v3/{project_id}`.

Use `volumes.List` for `GET /volumes/detail` and `volumes.Get` for
`GET /volumes/{volume_id}` on that endpoint. Use the project's `X-Auth-Token`.
The sandbox starts with an unattached `fixture-volume` of 1 GiB. The
scenario uses baseline Cinder v3 metadata and unfiltered reads.

## Create and provision a blank volume

Use `volumes.Create` for `POST /volumes` on the project endpoint. The
`volume` object requires a positive size in GiB and accepts a name and
description. The default type is `standard` and the availability zone is
`nova`. The request returns `202` with a volume in status `creating`.
Both reads show that status until the test drives an outcome.

To make the volume available, take the ID from the create response and
call the SandStack control with the admin token.

```sh
complete_url="http://127.0.0.1:8080/_sandstack/volume/volumes/${volume_id}/complete-create"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$complete_url"
```

The control returns `204` and moves the volume to `available`. Repeating
it for an available volume keeps its update timestamp. To exercise a
failed create, inject the volume fault instead to move it to `error`.

Use `volumes.Delete` for `DELETE /volumes/{volume_id}`. A `creating`
volume models provisioning in progress and receives `400`. Deleting an
`available` or `error` volume returns `202` with an empty body. The volume
is absent from subsequent reads. Reset discards created volumes and
restores the initial fixture.

## Read the attachment state

After [Nova attaches a volume](compute.md#attach-and-detach-a-cinder-volume),
Cinder lists it as `in-use`. Its `attachments` array contains the server ID,
volume ID, assigned device, and attachment timestamp. The `id` is the volume
ID and `attachment_id` identifies this particular attachment. An `in-use`
volume returns `400` on Cinder delete and `409` when the volume fault control
tries to set it to `error`. After detach, the volume becomes `available` with
an empty `attachments` array. See the [volume details reference].

## Inject and clear a volume fault

Use the volume ID from the detailed list, or the seeded ID in this example.

```sh
volume_id=7b37635e-be9b-4bfb-bc2e-d163b75921e6
fault_url="http://127.0.0.1:8080/_sandstack/volume/volumes/${volume_id}/fault"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$fault_url"
```

The volume moves from `available` to `error`. Both reads still return
`200`, preserve its ID and size, and show the updated status. Send `DELETE`
to the control URL to restore `available`. [Reset the sandbox](reset.md)
to restore the original values and timestamps.

## Verification

Run `go test ./...` to exercise discovery, creation, provisioning, volume
reads, deletion, faults, and reset through Gophercloud. The HTTP tests use
the [create volume reference] and [delete volume reference].

[create volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#create-a-volume
[delete volume reference]: https://docs.openstack.org/api-ref/block-storage/v3/#delete-a-volume
[volume details reference]: https://docs.openstack.org/api-ref/block-storage/v3/#show-a-volume-s-details
