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
timestamps, and state. Image and flavor references are fixture values.

The detailed list supports a single `status` filter. Use `?status=ACTIVE`
or `?status=ERROR` to select a state. Values match without regard to case.
Omit `status` to list all fixture servers. An empty, nonmatching, or unknown
value produces an empty `servers` array, as in Nova v2.1. SandStack returns
`400` for other list parameters or a repeated `status` parameter.

## Inject and clear an instance fault

The server initially reports `ACTIVE`. Send a `PUT` with the SandStack admin
token to set its fault state.

```sh
server_id=d8ba25c2-8224-4e09-88ef-fab16c654770
fault_url="http://127.0.0.1:8080/_sandstack/compute/servers/${server_id}/fault"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$fault_url"
```

The control returns `204`. The server detail and the `ERROR` list now show
an instance fault with code `500`, a message, and its creation time. The
`ACTIVE` list is empty. Repeating the control preserves the current fault.
Send `DELETE` to the same URL to return the fixture to `ACTIVE` and remove
the fault.

## Verification

Run `go test ./...` to check catalog discovery and the `ACTIVE` → `ERROR` →
`ACTIVE` sequence through Gophercloud. The raw HTTP checks cover the list and
detail response shapes and errors. The [Compute list reference],
[Compute detail reference], and [Compute fault guide] describe the upstream
behavior.

[Compute list reference]: https://docs.openstack.org/api-ref/compute/#list-servers-detailed
[Compute detail reference]: https://docs.openstack.org/api-ref/compute/#show-server-details
[Compute fault guide]: https://docs.openstack.org/api-guide/compute/faults.html
