# Block Storage sandbox

Use the Block Storage sandbox to test a client that discovers a volume and
handles a storage error. Authenticate with the [Identity fixture](identity.md).
Gophercloud's `NewBlockStorageV3` selects the public `block-storage` endpoint
in `RegionOne`. The catalog endpoint is `/volume/v3/{project_id}`.

Use `volumes.List` for `GET /volumes/detail` and `volumes.Get` for
`GET /volumes/{volume_id}` on that endpoint. Use the project's `X-Auth-Token`.
The fixture has one unattached `fixture-volume` with a reported size of
1 GiB. The scenario uses baseline Cinder v3 metadata and unfiltered reads.

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

Run `go test ./...` to exercise discovery, volume reads, fault transitions,
and reset through Gophercloud. The HTTP tests use the
[Block Storage v3 API reference].

[Block Storage v3 API reference]: https://docs.openstack.org/api-ref/block-storage/v3/
