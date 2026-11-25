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
| `GET /network/v2.0/networks` | Lists the project's private networks, including any networks the client has created. Use `networks.List` in Gophercloud. |
| `GET /network/v2.0/networks/{network_id}` | Returns one network. Use `networks.Get` in Gophercloud. Unknown network IDs return `404`. |
| `POST /network/v2.0/networks` | Creates a private network for the token's project and returns `201`. Use `networks.Create` in Gophercloud. |
| `DELETE /network/v2.0/networks/{network_id}` | Removes a network owned by the token's project and returns `204`. Use `networks.Delete` in Gophercloud. |

Use project-scoped `X-Auth-Token` credentials. All networks are private and
start with no subnets. [Reset the sandbox](reset.md) to restore the seed and
remove networks created by the client.

## Create a network

Send a `network` object with `name` and `admin_state_up` as needed. A name
may be empty or contain up to 255 printable characters with no surrounding
whitespace. The administrative state defaults to up. You can also pass
`shared: false` and set `project_id` or `tenant_id` to the token's project.
A network created with `admin_state_up: false` reports `DOWN`.

## Find a network by name or status

The list accepts `name` and `status` filters, including both in a single
request. Matches are exact. For example, `?name=fixture-network&status=DOWN`
selects that network while it is down. A repeated key selects any of its
values. For example, `?status=ACTIVE&status=DOWN` selects either status.
Omit a key to leave that criterion unrestricted. An explicit `name=`
matches unnamed networks, and `status=` matches no networks. A list
without matches returns `{"networks":[]}`.

Use `Name` and `Status` in Gophercloud's `networks.ListOpts`. SandStack
returns `400` for other list query parameters and for query parameters on
network detail, creation, and deletion.

## Inject and clear a network outage

Use a created or listed network ID. This example uses the seeded ID.

```sh
network_id=43d92fea-ce36-4d02-aba4-29806839f902
fault_url="http://127.0.0.1:8080/_sandstack/network/networks/${network_id}/fault"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$fault_url"
```

Both reads now report `DOWN`. An administratively enabled network keeps
`admin_state_up: true`. Send `DELETE` to the same URL to clear the outage,
or [reset the sandbox](reset.md) to restore all fixtures.

## Verification

Run `go test ./...` to check endpoint discovery, creation, filtered list and
detail reads, the injected outage, deletion, and reset through Gophercloud.
The raw HTTP tests use the [Neutron filtering reference], [Neutron list reference],
[Neutron detail reference], [Neutron create reference], and [Neutron delete reference].

[Neutron filtering reference]: https://docs.openstack.org/api-ref/network/v2/#filtering-and-column-selection
[Neutron list reference]: https://docs.openstack.org/api-ref/network/v2/#list-networks
[Neutron detail reference]: https://docs.openstack.org/api-ref/network/v2/#show-network-details
[Neutron create reference]: https://docs.openstack.org/api-ref/network/v2/#create-network
[Neutron delete reference]: https://docs.openstack.org/api-ref/network/v2/#delete-network
