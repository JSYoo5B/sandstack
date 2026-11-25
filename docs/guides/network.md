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
remove networks, subnets, and ports created by the client.

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

## Add an IPv4 subnet

The subnet collection supports `POST` and `GET` at
`/network/v2.0/subnets`. Use `GET` or `DELETE` on that path followed by the
subnet ID to read or remove a subnet. Gophercloud provides `subnets.Create`,
`subnets.List`, `subnets.Get`, and `subnets.Delete` for these requests.

Create a subnet on a network owned by the token's project. Send `network_id`,
`ip_version: 4`, and an IPv4 `cidr` with a prefix length from 1 through 30.
The optional name defaults to an empty string and `enable_dhcp` defaults to
`true`. SandStack normalizes the prefix, uses the first usable address as the
gateway, and derives one allocation pool from the remaining usable
addresses. For `192.0.2.0/24`, the gateway is `192.0.2.1` and the pool is
`192.0.2.2` through `192.0.2.254`. Custom gateways and pools are outside
this scenario. Overlapping subnets on one network receive `400`.

Network reads include the IDs of their subnets. Deleting a subnet removes
its ID from the network. Deleting a network with no ports also deletes its subnets.
The subnet list is unfiltered. A sandbox reset clears created subnets.

## Allocate and release a port address

Use `POST /network/v2.0/ports` with a `port` object containing `network_id`.
The optional `name` defaults to empty and `admin_state_up` defaults to `true`.
SandStack assigns a locally administered MAC address. The unbound port
reports `DOWN` and has empty `device_id` and `device_owner` fields.

With one subnet on the network, omitting `fixed_ips` assigns the first free
address in its pool. With no subnets, the port has no fixed IP. If the
network has several subnets, pass one entry such as
`"fixed_ips":[{"subnet_id":"SUBNET_ID"}]` to select a pool. Pass
`"fixed_ips":[]` to create a port without an address. An exhausted pool
returns `409`. The fixture supports one IPv4 allocation per port.

Use `GET /network/v2.0/ports` for the project's port list. An optional
`network_id` query parameter filters the list, and repeats match any of
the requested network IDs. Use `GET` or `DELETE` on the path followed by
the port ID to read or remove the port. The corresponding Gophercloud
operations are `ports.Create`, `ports.List`, `ports.Get`, and `ports.Delete`.

Deleting the port returns its IP to the pool. Network deletion returns
`409` while ports remain on it. Subnet deletion returns `409` while a port
holds an IP from it. Reset clears ports and their allocations.

## Manage an unattached router

Use the project's `X-Auth-Token` on the v2.0 network endpoint. SandStack
creates each router in the token's project. A supplied `project_id` or
`tenant_id` must match that project. Send a `router` object with an optional
`name`, `description`, and `admin_state_up`. The name and description
default to empty strings and the administrative state defaults to up. The
router starts with `status: ACTIVE` even if you create it with
`admin_state_up: false`. See [Neutron router initialization].

| Request | Gophercloud call | Result |
| --- | --- | --- |
| `POST /network/v2.0/routers` | `routers.Create` | `201` with the created router. |
| `GET /network/v2.0/routers` | `routers.List` | `200` with an unfiltered list of the project's routers. |
| `GET /network/v2.0/routers/{router_id}` | `routers.Get` | `200` with the router's current metadata. |
| `DELETE /network/v2.0/routers/{router_id}` | `routers.Delete` | `204` and the router disappears from subsequent reads. |

List query parameters return `400`. Router responses show
`external_gateway_info: null`, `gw_port_id: null`, and `routes: []`.
Gateway configuration, interface attachment, router updates, and packet
routing are outside this scope. [Reset the sandbox](reset.md) to clear the
created routers.

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

Run `go test ./...` to check discovery, network, subnet, port, and router
lifecycle, filtered lists, the injected outage, and reset through Gophercloud.
The raw HTTP tests use the [Neutron filtering reference], [Neutron list reference],
[Neutron detail reference], [Neutron create reference], [Neutron delete reference],
[subnet create reference], and [port create reference]. The [Neutron database
implementation] and [exception types] explain deletion and exhaustion conflicts.
The router tests use the [router create reference], [router list reference],
[router details reference], and [router delete reference].

[Neutron filtering reference]: https://docs.openstack.org/api-ref/network/v2/#filtering-and-column-selection
[Neutron list reference]: https://docs.openstack.org/api-ref/network/v2/#list-networks
[Neutron detail reference]: https://docs.openstack.org/api-ref/network/v2/#show-network-details
[Neutron create reference]: https://docs.openstack.org/api-ref/network/v2/#create-network
[Neutron delete reference]: https://docs.openstack.org/api-ref/network/v2/#delete-network
[subnet create reference]: https://docs.openstack.org/api-ref/network/v2/#create-subnet
[port create reference]: https://docs.openstack.org/api-ref/network/v2/#create-port
[router create reference]: https://docs.openstack.org/api-ref/network/v2/#create-router
[router list reference]: https://docs.openstack.org/api-ref/network/v2/#list-routers
[router details reference]: https://docs.openstack.org/api-ref/network/v2/#show-router-details
[router delete reference]: https://docs.openstack.org/api-ref/network/v2/#delete-router
[Neutron router initialization]: https://github.com/openstack/neutron/blob/master/neutron/db/l3_db.py#L218-L256
[Neutron database implementation]: https://github.com/openstack/neutron/blob/master/neutron/db/db_base_plugin_v2.py
[exception types]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/exceptions/__init__.py
