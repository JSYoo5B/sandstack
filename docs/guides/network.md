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

Use `GET /network/v2.0/ports` for the project's port list. The `network_id`,
`device_id`, and `device_owner` filters match exact values. Repeating a key
matches any of its values. Combining different keys requires a match for
each key. Omit a key to leave it unrestricted. In a raw query, `device_id=`
or `device_owner=` matches ports with the corresponding empty field.
Unsupported query keys return `400`. See the [port list reference] and
[Neutron filtering reference].

Use `GET` or `DELETE` on the path followed by the port ID to read or remove
the port. The corresponding Gophercloud operations are `ports.Create`,
`ports.List`, `ports.Get`, and `ports.Delete`. Use `NetworkID`, `DeviceID`,
and `DeviceOwner` in `ports.ListOpts`.

Deleting an unbound port releases its IP. Network deletion returns
`409` while ports remain on it. Subnet deletion returns `409` while a port
holds an IP from it. Reset clears ports and their allocations.

### Request a specific IPv4 address

Send one `fixed_ips` entry with `ip_address` to request that address. You
can also include `subnet_id` to select a subnet on the project's network.
If you omit it, SandStack selects the subnet on that network that contains
the address. A network address, broadcast address, or address outside the
selected subnet receives `400`. An already allocated address receives
`409`. See the [port create reference] and [Neutron IP allocation validation].

For a subnet containing `192.0.2.10`, use `ports.Create` with
`ports.CreateOpts.FixedIPs` in Gophercloud.

```go
port, err := ports.Create(ctx, client, ports.CreateOpts{
    NetworkID: networkID,
    FixedIPs:  []ports.IP{{SubnetID: subnetID, IPAddress: "192.0.2.10"}},
}).Extract()
```

Leave `SubnetID` out of the entry to select the subnet by address. To claim
an unused subnet gateway, request that address explicitly. Automatic
allocation skips the gateway. A router interface and a port requesting the
same gateway conflict with `409`. Deleting an unbound port releases its
address so it can be requested again.

### Update a port

Use `PUT /network/v2.0/ports/{port_id}` with a `port` object containing
`name`, `admin_state_up`, or `fixed_ips`. The response is `200` with the
updated port. Omitted fields retain their values. To clear a name or set
the administrative state down, send an empty string or `false` explicitly.
See the [port update reference].

Omit `fixed_ips` to keep the current allocation or send `[]` to release
it. Send one entry to select an IPv4 address or subnet on the port's
current network. If the entry specifies only the subnet that already
holds the port's address, the existing address stays allocated. An
allocation conflict returns `409` and preserves both the previous address
and the port metadata. SandStack protects managed router interface
reservations from direct port address updates. See the [Neutron IP
allocation update].

In Gophercloud, use `ports.Update`. Pass a non-nil empty slice to send
`fixed_ips: []`.

```go
name, adminUp := "unaddressed-port", false
port, err := ports.Update(ctx, client, portID, ports.UpdateOpts{
    Name:         &name,
    AdminStateUp: &adminUp,
    FixedIPs:     []ports.IP{},
}).Extract()
```

## Manage a router

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
| `PUT /network/v2.0/routers/{router_id}` | `routers.Update` | `200` with updated router metadata. |
| `DELETE /network/v2.0/routers/{router_id}` | `routers.Delete` | `204` and the router disappears from subsequent reads. |

List query parameters return `400`. Router responses show
`external_gateway_info: null` and `gw_port_id: null`. A new router starts
with `routes: []`.

### Update router metadata

Send a `router` object containing any of `name`, `description`, or
`admin_state_up`. Fields you omit keep their current values, and an empty
`router` object returns the current metadata. A supplied empty string
clears the name or description. An explicit `false` sets the administrative
state to down. The sandbox reports `ACTIVE` independently of the
administrative state. Updates retain project ownership and existing
interface attachments. See the [router update reference].

Use `routers.UpdateOpts` with `routers.Update`. Pass a pointer to an empty
string or `false` to send those values in `Description` or `AdminStateUp`
respectively. To clear the router name, send `{"router":{"name":""}}` as
a raw request. Gophercloud omits an empty `UpdateOpts.Name`.

### Replace static IPv4 routes

Use `PUT /network/v2.0/routers/{router_id}` with `routes` in the `router`
object. In Gophercloud, pass a pointer to a `[]routers.Route` as
`UpdateOpts.Routes` in `routers.Update`. The supplied list replaces all
current routes. Omit `routes` to preserve them. Use an empty slice, or send
`routes: []` or `routes: null` over HTTP, to clear them. Read the resulting
routes with `routers.Get` or `routers.List`. See the [router update reference]
and [Neutron route attribute definition].

Each route requires a network-aligned IPv4 prefix as `destination` and an
IPv4 address as `nexthop`. The next hop must belong to an attached subnet
and differ from the router's own interface addresses. An invalid route
returns `400` and rejects the entire update. The fixture models route
configuration. Packet forwarding is outside this scope. See
[Neutron extra route validation].

### Add or remove selected extra routes

Use `PUT /network/v2.0/routers/{router_id}/add_extraroutes` with Gophercloud's
`extraroutes.Add` or `PUT /network/v2.0/routers/{router_id}/remove_extraroutes`
with `extraroutes.Remove`. Supply a pointer to a `[]routers.Route` in
`extraroutes.Opts.Routes`. Both requests require a `routes` array inside the
`router` object. An empty array is a no-op. Each response returns `200` with
a `router` object containing the resulting routes. See the
[add extra routes reference] and [remove extra routes reference].

The actions match each route by its destination and next hop and preserve
unrelated routes. Adding a pair that already exists or removing an absent
pair succeeds without a change. Multiple routes can share a destination if
their next hops differ. The same route validation applies to the resulting
set. An invalid request returns `400` and leaves the router's previous
routes intact. See [Neutron extra route member actions].

## Attach and remove an IPv4 router interface

Use the project token for both interface actions. The router and its subnet
or port must belong to that project. The request fields are top-level
`subnet_id` and `port_id`.

- `PUT /network/v2.0/routers/{router_id}/add_router_interface` uses
  Gophercloud's `routers.AddInterface` with exactly one of `SubnetID` and
  `PortID` in `AddInterfaceOpts`. An HTTP request with both or neither
  returns `400`.
- `PUT /network/v2.0/routers/{router_id}/remove_router_interface` uses
  `routers.RemoveInterface` with one or both fields in
  `RemoveInterfaceOpts`. If you send both, they must identify the same
  interface. A mismatch returns `400` with `NeutronError.type` set to
  `SubnetMismatchForPort`.

The [remove interface reference] describes `409` for mismatched selectors.
SandStack follows Neutron's implementation. `SubnetMismatchForPort`
inherits from `BadRequest`, which maps to HTTP `400`. See the
[Neutron subnet mismatch exception] and [Neutron HTTP fault mapping].

Both actions return `200` with an unwrapped interface object. Its `id`
is the router's ID, `port_id` identifies the interface port, and
`subnet_ids` contains the affected subnet's ID.

### Attach by subnet ID

SandStack creates a port with the subnet's gateway address as its fixed
IP. A subnet gateway already in use by another router returns `409`. See
[Neutron interface creation].

### Attach by port ID

Pass an existing unbound port with one IPv4 fixed IP. The port keeps its
original ID, fixed IP, and MAC address. A port that is already attached
returns `409`. A port with no fixed IP returns `400`. See
[Neutron port validation] and [Neutron attachment by port].

### Inspect and remove the interface

Use `ports.Get` on the returned `port_id`. To discover a router's managed
interface ports, call `ports.List` with `DeviceID` set to the router ID and
`DeviceOwner` set to `network:router_interface` in `ports.ListOpts`.
Adding a new interface on a subnet already present on the router, or on a
subnet that overlaps one already present, returns `400`. See
[Neutron router subnet checks].

While the interface exists, deleting its port, router, subnet, or network
directly returns `409`. Remove the interface first. Removing it deletes
the interface port and releases its fixed IP, including when you supplied
an existing port. The remaining resources can then be deleted if their
other dependencies are clear. See [Neutron interface removal] and
[Neutron removal by port].

Interface removal returns `409` while a route uses that interface's subnet
for its next hop. Replace or clear those routes before removing the
interface. See [Neutron extra route validation].

Each interface has one IPv4 fixed IP. External gateways and packet
forwarding are outside this scope.
[Reset the sandbox](reset.md) to clear the created routers, interfaces, and
ports.

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
[router details reference], [router update reference], and
[router delete reference]. Route tests check replacement, atomic addition
and removal, validation, and blocked interface removal. Interface tests
cover both attachment selectors, port identity, allocation reuse, and
blocked deletions using the [add interface reference] and
[remove interface reference].

[Neutron filtering reference]: https://docs.openstack.org/api-ref/network/v2/#filtering-and-column-selection
[Neutron list reference]: https://docs.openstack.org/api-ref/network/v2/#list-networks
[Neutron detail reference]: https://docs.openstack.org/api-ref/network/v2/#show-network-details
[Neutron create reference]: https://docs.openstack.org/api-ref/network/v2/#create-network
[Neutron delete reference]: https://docs.openstack.org/api-ref/network/v2/#delete-network
[subnet create reference]: https://docs.openstack.org/api-ref/network/v2/#create-subnet
[port create reference]: https://docs.openstack.org/api-ref/network/v2/#create-port
[port list reference]: https://docs.openstack.org/api-ref/network/v2/#list-ports
[port update reference]: https://docs.openstack.org/api-ref/network/v2/#update-port
[Neutron IP allocation validation]: https://github.com/openstack/neutron/blob/master/neutron/ipam/drivers/neutrondb_ipam/driver.py#L125-L141
[Neutron IP allocation update]: https://github.com/openstack/neutron/blob/master/neutron/db/ipam_pluggable_backend.py#L439-L506
[router create reference]: https://docs.openstack.org/api-ref/network/v2/#create-router
[router list reference]: https://docs.openstack.org/api-ref/network/v2/#list-routers
[router details reference]: https://docs.openstack.org/api-ref/network/v2/#show-router-details
[router update reference]: https://docs.openstack.org/api-ref/network/v2/#update-router
[router delete reference]: https://docs.openstack.org/api-ref/network/v2/#delete-router
[add extra routes reference]: https://docs.openstack.org/api-ref/network/v2/#add-extra-routes-to-router
[remove extra routes reference]: https://docs.openstack.org/api-ref/network/v2/#remove-extra-routes-from-router
[add interface reference]: https://docs.openstack.org/api-ref/network/v2/#add-interface-to-router
[remove interface reference]: https://docs.openstack.org/api-ref/network/v2/#remove-interface-from-router
[Neutron router initialization]: https://github.com/openstack/neutron/blob/master/neutron/db/l3_db.py#L218-L256
[Neutron route attribute definition]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/api/definitions/extraroute.py#L33-L40
[Neutron extra route validation]: https://github.com/openstack/neutron/blob/master/neutron/db/extraroute_db.py#L76-L155
[Neutron extra route member actions]: https://github.com/openstack/neutron/blob/master/neutron/db/extraroute_db.py#L157-L231
[Neutron interface creation]: https://github.com/openstack/neutron/blob/master/neutron/db/l3_db.py#L838-L898
[Neutron port validation]: https://github.com/openstack/neutron/blob/master/neutron/db/l3_db.py#L700-L769
[Neutron attachment by port]: https://github.com/openstack/neutron/blob/master/neutron/db/l3_db.py#L797-L804
[Neutron router subnet checks]: https://github.com/openstack/neutron/blob/master/neutron/db/l3_db.py#L650-L678
[Neutron interface removal]: https://github.com/openstack/neutron/blob/master/neutron/db/l3_db.py#L1099-L1122
[Neutron removal by port]: https://github.com/openstack/neutron/blob/master/neutron/db/l3_db.py#L1061-L1080
[Neutron subnet mismatch exception]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/exceptions/__init__.py#L349-L357
[Neutron HTTP fault mapping]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/api/faults.py#L16-L20
[Neutron database implementation]: https://github.com/openstack/neutron/blob/master/neutron/db/db_base_plugin_v2.py
[exception types]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/exceptions/__init__.py
