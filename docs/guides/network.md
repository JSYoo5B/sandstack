# Networking sandbox

Use the Networking sandbox to test how a client reacts when a Neutron
network is administratively enabled but operationally down. Authenticate
using the [Identity fixture](identity.md). Gophercloud selects the public
`network` endpoint in `RegionOne` through `NewNetworkV2`. Its catalog entry
ends in `/network`. The client discovers v2.0 there and appends `/v2.0` for
resource requests.

| Request | Behavior |
| --- | --- |
| `GET /network/` | Discovers the current v2.0 API without credentials. |
| `GET /network/v2.0/networks` | Lists the project's private fixture network. Use `networks.List` in Gophercloud. |
| `GET /network/v2.0/networks/{network_id}` | Returns one network. Use `networks.Get` in Gophercloud. Unknown network IDs return `404`. |

Use project-scoped `X-Auth-Token` credentials. Both operations support
unfiltered reads. SandStack returns `400` for query parameters. The network
has no subnets and `shared` is `false`.

## Inject and clear a network outage

Use the listed network ID, or the seeded ID in this example.

```sh
network_id=43d92fea-ce36-4d02-aba4-29806839f902
fault_url="http://127.0.0.1:8080/_sandstack/network/networks/${network_id}/fault"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$fault_url"
```

Both reads now report `DOWN` with `admin_state_up: true`. Send `DELETE`
to the same URL to restore `ACTIVE`, or [reset the sandbox](reset.md) to
restore all fixtures.

## Verification

Run `go test ./...` to check endpoint discovery, list and detail reads,
the injected outage, and reset through Gophercloud. The raw HTTP tests use
the [Neutron list reference] and [Neutron detail reference].

[Neutron list reference]: https://docs.openstack.org/api-ref/network/v2/#list-networks
[Neutron detail reference]: https://docs.openstack.org/api-ref/network/v2/#show-network-details
