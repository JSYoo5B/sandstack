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
timestamps, and state. The image ID refers to the [Glance fixture](image.md).
The flavor reference is a fixture value.

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

## Make a detail read stale

Start with an `ACTIVE` server. Send `PUT` to the `stale-detail` control to
capture its current detail, then inject the instance fault.

```sh
stale_url="${fault_url%/fault}/stale-detail"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$stale_url"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$fault_url"
```

Both controls return `204`. `GET /servers/{server_id}` keeps returning the
captured `ACTIVE` detail, including its original timestamp. The detailed
list shows `ERROR` and the current instance fault. Repeating `PUT` on
`stale-detail` keeps the original snapshot. Send `DELETE` to `stale-detail`
to resume live detail reads. The live fault can then be cleared separately.

## Fail the next detail read

Use a one-shot outage to exercise a client's retry after HTTP `503`. Send
`PUT` to the `unavailable-detail` control for the listed server ID.

```sh
outage_url="${fault_url%/fault}/unavailable-detail"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$outage_url"
```

The next authorized detail request for that server receives `503` with a
`serviceUnavailable` error. A retry reads the same server state with the
same token. List requests and rejected detail requests leave the outage
pending. Repeating `PUT` while the outage is pending keeps one failure
scheduled. `DELETE` or a [sandbox reset](reset.md) clears it.

Inspect [recent requests](requests.md) to check that the attempted detail
read received `503` before the same route returned `200`.

## Verification

Run `go test ./...` to check catalog discovery and the `ACTIVE` → `ERROR` →
`ACTIVE` sequence through Gophercloud. The stale-detail test also checks
that list and detail diverge while the snapshot is active and agree after
it is cleared. The transient-outage test verifies one failure followed by
recovery. The raw HTTP checks use the [Compute list reference],
[Compute detail reference], [Compute fault guide], and [Nova fault response]
for the OpenStack response shapes.

[Compute list reference]: https://docs.openstack.org/api-ref/compute/#list-servers-detailed
[Compute detail reference]: https://docs.openstack.org/api-ref/compute/#show-server-details
[Compute fault guide]: https://docs.openstack.org/api-guide/compute/faults.html
[Nova fault response]: https://github.com/openstack/nova/blob/master/nova/api/openstack/wsgi.py
