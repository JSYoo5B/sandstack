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

A router without a gateway shows `external_gateway_info: null`. Responses
omit `gw_port_id` because Neutron removes attributes that no API
definition declares. See [Set an external gateway](#set-an-external-gateway)
and [Neutron response attribute filtering]. A new router starts with
`routes: []`.

### Filter the router list

Filter the token's routers by exact `name` or `admin_state_up`. A repeated
key matches any of its values. Supplying both keys requires both fields
to match. Omitted keys leave those fields unrestricted. SandStack accepts
these filters and the pagination options below. A `status` query returns
`400`. The fixture reports operational `status: ACTIVE` for both
administrative states. See the [router list reference] and [Neutron router
filter schema].

In Gophercloud v2.15.0, pass `Name` and `AdminStateUp` in
`routers.ListOpts` to `routers.List`. Set `AdminStateUp` to a pointer to
`false` to find administratively disabled routers. Leave it nil to
include both states.

### Page through the router list

Use a positive `limit` to page through matches by ascending router ID.
The optional `marker` starts the page after that router. Set
`page_reverse=true` to get the page before the marker, or omit the marker
to get the last page. Both directions return routers in ascending ID
order. A router visible to the token can serve as a marker even if the
current filters exclude it. Unknown or inaccessible markers return
`404`. With `limit` omitted or set to `0`, the response is unpaged and
ignores the marker and direction. Repeated pagination keys and invalid
limits return `400`. Only `true`, matched without regard to letter case,
enables reverse navigation. See the [Neutron pagination reference] and
[Neutron router marker lookup].

For a nonempty page, `routers_links` contains an absolute `next` link if
later matches exist and an absolute `previous` link if earlier matches
exist. Both links preserve the current filters, repeated filter values,
and the page size. `next` uses the last router ID as the marker and omits
`page_reverse`. `previous` uses the first router ID and sets
`page_reverse=True`. Empty and unpaged responses omit navigation links.

In Gophercloud v2.15.0, set `Limit` and optionally `Marker` in
`routers.ListOpts`. The pager from `routers.List` follows `next` links
when you call `AllPages`. For reverse navigation, read `routers_links`
from the raw response and pass a `previous` URL to `ServiceClient.Get`.
See the [Gophercloud router list options] and [Gophercloud router page links].

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

## Set an external gateway

Discover the fixture external network with `GET
/network/v2.0/networks?router:external=true`. In Gophercloud v2.15.0,
wrap `networks.ListOpts` in `external.ListOptsExt` with `External` set to a
pointer to `true`, and decode `external.NetworkExternalExt` with
`networks.ExtractNetworksInto`. The network is named `public`, reports
`router:external: true` and `shared: false`, and belongs to a separate
administrative project. It has one subnet, `203.0.113.0/24`, with gateway
`203.0.113.1` and allocation pool `203.0.113.2` through `203.0.113.254`.
Every project can read the network. Updating or deleting it returns `403`
with `PolicyNotAuthorized`, and creating a port on it returns `404`. See
the [Neutron external network definition], [Neutron network read policy],
[Neutron network write policy], [Neutron update and delete authorization],
and [Neutron network ownership check].

Set `external_gateway_info` to `{"network_id": "<external network ID>"}`
when you create or update a router. In Gophercloud, pass
`routers.GatewayInfo{NetworkID: id}` as `GatewayInfo` in
`routers.CreateOpts` or `routers.UpdateOpts`. The response then shows the
network ID, `enable_snat: true`, and `external_fixed_ips` with the
allocated external subnet address. Addresses come from the lowest free
address in the external allocation pool. The gateway port belongs to no project, so port reads and lists in the
router's project do not show it. Setting the same network again keeps the
current address. See the [router update reference], [Neutron gateway
response], [Neutron gateway mode response], and [Neutron SNAT default].

Send `external_gateway_info: null` or `{}` to clear the gateway and release
its address. In Gophercloud, pass `&routers.GatewayInfo{}`. Deleting a router
also releases its gateway address. See [Neutron gateway replacement] and
[Neutron router deletion].

| Request | Result |
| --- | --- |
| Unknown or inaccessible `network_id` | `404` with `NetworkNotFound`. |
| Network without `router:external` | `400` with `BadRequest`. |
| `network_id` missing or not a UUID | `400` with `BadRequest`. |
| `enable_snat` or `external_fixed_ips` | `403` with `PolicyNotAuthorized`. Neutron reserves these options for administrators. |
| External subnet overlaps a router interface subnet | `400`. The reverse order, attaching an overlapping interface after the gateway, also returns `400`. |
| Clearing a gateway that a static route uses for its next hop | `400` with `InvalidRoutes`. The gateway remains. |
| External allocation pool is exhausted | `409` with `IpAddressGenerationFailure`. A router creation request creates nothing. |
| Clearing or replacing a gateway that serves floating IPs | `409` with `RouterExternalGatewayInUseByFloatingIp`. See [Dependent resources](network-floating-ips.md#dependent-resources). |

See the [Neutron gateway attribute], [Neutron gateway mode attribute],
[Neutron router gateway policy], [Neutron router gateway update policy],
[Neutron gateway validation], and [Neutron gateway subnet checks]. With a
gateway, a static route can use a next hop on the external subnet other
than the router's own gateway address. See [Neutron route validation].

Every project can read the external subnet through subnet lists and
details, but cannot update or delete it. See
[Read the external subnet](network-subnets.md#read-the-external-subnet).
Multiple external gateways and packet forwarding are outside this section.
To give a port a public address through the gateway, see
[Floating IPs](network-floating-ips.md).

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
Interface removal also returns `409` while a floating IP is associated
through that interface. See [Dependent
resources](network-floating-ips.md#dependent-resources).

Each interface has one IPv4 fixed IP. Packet forwarding is outside this
scope.
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
[remove interface reference]. Gateway tests cover external network
discovery and ownership, address allocation and release, administrator-only
options, overlap and route checks, and reset.

[router create reference]: https://docs.openstack.org/api-ref/network/v2/#create-router
[router list reference]: https://docs.openstack.org/api-ref/network/v2/#list-routers
[Neutron pagination reference]: https://docs.openstack.org/api-ref/network/v2/#pagination
[Neutron router marker lookup]: https://github.com/openstack/neutron/blob/master/neutron/db/l3_db.py#L629-L645
[Gophercloud router list options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/networking/v2/extensions/layer3/routers/requests.go#L22-L39
[Gophercloud router page links]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/networking/v2/extensions/layer3/routers/results.go#L131-L149
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
[Neutron response attribute filtering]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/api/v2/base.py#L159-L192
[Neutron external network definition]: https://github.com/openstack/neutron-lib/blob/5.0.0/neutron_lib/api/definitions/external_net.py#L37-L50
[Neutron network read policy]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/conf/policies/network.py#L195-L216
[Neutron network write policy]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/conf/policies/network.py#L283-L294
[Neutron update and delete authorization]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/api/v2/base.py#L577-L598
[Neutron network ownership check]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/api/v2/base.py#L777-L796
[Neutron gateway response]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L237-L256
[Neutron gateway mode response]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_gwmode_db.py#L35-L76
[Neutron SNAT default]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/conf/db/l3_gwmode_db.py#L19-L23
[Neutron gateway replacement]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L464-L586
[Neutron router deletion]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L639-L681
[Neutron gateway attribute]: https://github.com/openstack/neutron-lib/blob/5.0.0/neutron_lib/api/definitions/l3.py#L92-L104
[Neutron gateway mode attribute]: https://github.com/openstack/neutron-lib/blob/5.0.0/neutron_lib/api/definitions/l3_ext_gw_mode.py#L39-L50
[Neutron router gateway policy]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/conf/policies/router.py#L105-L157
[Neutron router gateway update policy]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/conf/policies/router.py#L272-L322
[Neutron gateway validation]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L413-L457
[Neutron route validation]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/extraroute_db.py#L83-L124
[Neutron gateway subnet checks]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L709-L738
