# Compute sandbox

Use the Compute sandbox to test how a client reacts when a running server
reports an instance fault. Authenticate using the
[Identity fixture](identity.md). The project contains `fixture-server`,
with ID `d8ba25c2-8224-4e09-88ef-fab16c654770`. Use the public `compute`
endpoint in `RegionOne` from the service catalog and call
`GET /servers/{server_id}` on that endpoint. Gophercloud's `NewComputeV2`
and `servers.Get` use this path.

The catalog uses `/compute/v2.1/{project_id}`. Both the project-qualified
and projectless server URLs accept an `X-Auth-Token` from the fixture project.
The response uses baseline Nova v2.1 fields for server identity, ownership,
timestamps, and state. The scenario covers server-detail reads. Image and
flavor references are fixture values.

## Inject and clear an instance fault

The server initially reports `ACTIVE`. Send a `PUT` with the SandStack admin
token to set its fault state.

```sh
server_id=d8ba25c2-8224-4e09-88ef-fab16c654770
fault_url="http://127.0.0.1:8080/_sandstack/compute/servers/${server_id}/fault"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$fault_url"
```

The control returns `204`. Reading the same server still returns `200` and
now reports `ERROR` with a fault object containing code `500`, a message,
and its creation time. Repeating the control preserves the current fault.
Send `DELETE` to the same URL to return the fixture to `ACTIVE` and remove
the fault. These controls change only the fixture state.

## Verification

Run `go test ./...` to check catalog discovery and the `ACTIVE` → `ERROR` →
`ACTIVE` sequence through Gophercloud, and check the wire response and error
statuses over HTTP. The Nova server-detail response and conditional fault
object are based on the [Compute API reference] and [Compute fault guide].

[Compute API reference]: https://docs.openstack.org/api-ref/compute/#show-server-details
[Compute fault guide]: https://docs.openstack.org/api-guide/compute/faults.html
