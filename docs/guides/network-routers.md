# Routers in the Networking sandbox

[Networking sandbox](network.md)

Use the [Identity fixture](identity.md) to authenticate. Prepare an
[IPv4 subnet](network-subnets.md#add-an-ipv4-subnet) or a
[port with a fixed IP](network-ports.md#allocate-and-release-a-port-address)
before attaching an interface.

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
| `GET /network/v2.0/routers` | `routers.List` | `200` with the project's routers matching any supplied filters. |
| `GET /network/v2.0/routers/{router_id}` | `routers.Get` | `200` with the router's current metadata. |
| `PUT /network/v2.0/routers/{router_id}` | `routers.Update` | `200` with updated router metadata. |
| `DELETE /network/v2.0/routers/{router_id}` | `routers.Delete` | `204` and the router disappears from subsequent reads. |

Router responses show
`external_gateway_info: null` and `gw_port_id: null`. A new router starts
with `routes: []`.

### Filter the router list

Filter the token's routers by exact `name` or `admin_state_up`. A repeated
key matches any of its values. Supplying both keys requires both fields
to match. Omitted keys leave those fields unrestricted. SandStack returns
`400` for other query keys, including `status`. The fixture reports
operational `status: ACTIVE` for both administrative states. See the
[router list reference] and [Neutron router filter schema].

In Gophercloud v2.15.0, pass `Name` and `AdminStateUp` in
`routers.ListOpts` to `routers.List`. Set `AdminStateUp` to a pointer to
`false` to find administratively disabled routers. Leave it nil to
include both states.

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
IP. The subnet must have a usable gateway within its CIDR. A missing or
off-subnet gateway returns `400` for this attachment method. A subnet
gateway already in use by another router returns `409`. See [Neutron
interface creation].

### Attach by port ID

Pass an existing unbound port with one IPv4 fixed IP. The port keeps its
original ID, fixed IP, and MAC address. The port's fixed IP is used even
if the subnet has no usable gateway. A port that is already attached
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

## Verification

Run `go test ./...` to exercise the router scenarios through Gophercloud.
The router tests use the [router create reference], [router list reference],
[router details reference], [router update reference], and
[router delete reference]. Route tests check replacement, atomic addition
and removal, validation, and blocked interface removal. Interface tests
cover both attachment selectors, port identity, allocation reuse, and
blocked deletions using the [add interface reference] and
[remove interface reference].

[router create reference]: https://docs.openstack.org/api-ref/network/v2/#create-router
[router list reference]: https://docs.openstack.org/api-ref/network/v2/#list-routers
[Neutron router filter schema]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/api/definitions/l3.py#L65-L78
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
