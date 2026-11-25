# Nova fault controls

Use these controls to test server faults, stale detail, and one-shot read
failures. See the [Compute sandbox](compute.md) for authentication and
server queries. The stale attachment example uses the
[Cinder volume attachment workflow](compute.md#attach-and-detach-a-cinder-volume).

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
Send `DELETE` to the same URL to remove the fault and reveal the current
power state. Injecting and clearing an instance fault on a stopped server
shows `ERROR` and then `SHUTOFF`.

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

The snapshot also retains the attached volume IDs. Capture an unattached
server and then attach a volume to exercise a stale read. The detail still
shows an empty array, while the detailed list and standalone attachment
endpoints show the new attachment. Clearing `stale-detail` exposes the
current attachment IDs in the detail response.

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

An active stale-detail snapshot is used on retry after the outage is
consumed.

Inspect [recent requests](requests.md) to check that the attempted detail
read received `503` before the same route returned `200`.

## Verification

Run `go test ./...` to check catalog discovery and the `ACTIVE` → `ERROR` →
`ACTIVE` sequence through Gophercloud. The stale-detail test also checks
that list and detail diverge while the snapshot is active and agree after
it is cleared. The transient-outage test verifies one failure followed by
recovery. The attachment tests check both Nova and Cinder through the SDK
and verify the status codes and response fields directly over HTTP. They
also check the attached volume IDs in server detail and detailed list
through attach, detach, and stale snapshot recovery.

The raw HTTP checks use the [Compute list reference],
[Compute detail reference], [Compute fault guide], and [Nova fault response]
for the OpenStack response shapes.

[Compute list reference]: https://docs.openstack.org/api-ref/compute/#list-servers-detailed
[Compute detail reference]: https://docs.openstack.org/api-ref/compute/#show-server-details
[Compute fault guide]: https://docs.openstack.org/api-guide/compute/faults.html
[Nova fault response]: https://github.com/openstack/nova/blob/master/nova/api/openstack/wsgi.py
