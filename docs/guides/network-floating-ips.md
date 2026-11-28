# Floating IPs in the Networking sandbox

[Networking sandbox](network.md)

Use the [Identity fixture](identity.md) to authenticate. Give a server port
a public address in four steps:

1. Discover the external network and connect a router to it, as described
   in [Set an external gateway](network-routers.md#set-an-external-gateway).
2. Attach the port's IPv4 subnet to the same router, as described in
   [Attach and remove an IPv4 router interface](network-routers.md#attach-and-remove-an-ipv4-router-interface).
3. Create a port with a fixed IPv4 address on that subnet, as described in
   [Allocate and release a port address](network-ports.md#allocate-and-release-a-port-address).
4. Allocate a floating IP on the external network and associate it with
   the port.

## Allocate and associate a floating IP

Use the project's `X-Auth-Token`. Each floating IP belongs to the token's
project, and other projects cannot read or change it.

| Request | Gophercloud v2.15.0 call | Result |
| --- | --- | --- |
| `POST /network/v2.0/floatingips` | `floatingips.Create` | `201` with the new floating IP. |
| `GET /network/v2.0/floatingips` | `floatingips.List` | `200` with the project's floating IPs matching any filters. |
| `GET /network/v2.0/floatingips/{floatingip_id}` | `floatingips.Get` | `200` with one floating IP. |
| `PUT /network/v2.0/floatingips/{floatingip_id}` | `floatingips.Update` | `200` with the changed association or description. |
| `DELETE /network/v2.0/floatingips/{floatingip_id}` | `floatingips.Delete` | `204`. The address returns to the external pool. |

Send a `floatingip` object with the required `floating_network_id`. You can
also send `port_id`, `fixed_ip_address`, and `description`, and you can set
`project_id` or `tenant_id` to the token's project. In Gophercloud, set
`FloatingNetworkID`, `PortID`, `FixedIP`, and `Description` in
`floatingips.CreateOpts`. SandStack allocates the lowest free address in
the external allocation pool. Router gateways and floating IPs share that
pool. See the [floating IP create reference], [Neutron floating IP
attributes], and [Neutron floating IP creation].

Responses contain `id`, `project_id`, `tenant_id`, `description`,
`floating_network_id`, `floating_ip_address`, `router_id`, `port_id`,
`fixed_ip_address`, and `status`. An unassociated floating IP shows `null`
for `router_id`, `port_id`, and `fixed_ip_address`. See [Neutron floating
IP response].

### Associate with a port

Associating a port sets `port_id`, sets `fixed_ip_address` to the port's
fixed IPv4 address, and sets `router_id` to the router that connects the
port's subnet to the floating IP's external network. The port must belong
to the token's project. When several routers qualify, SandStack prefers
the router whose interface holds the subnet's gateway address and
otherwise picks the lowest router ID. Neutron does not define an order for
the remaining candidates. See [Neutron router selection] and [Neutron port
association checks].

To change the association, send `PUT` with `port_id` and an optional
`fixed_ip_address`. Send `port_id: null` to disassociate. In Gophercloud,
pass `PortID` as a pointer in `floatingips.UpdateOpts`. A pointer to an
empty string sends `null`. An update without `port_id` keeps the current
association. See the [floating IP update reference] and [Neutron
association update].

| Request | Result |
| --- | --- |
| Unknown or inaccessible `floating_network_id` | `404` with `NetworkNotFound`. |
| Network without `router:external` | `400` with `BadRequest`. |
| Unknown port or another project's port | `404` with `PortNotFound`. |
| `fixed_ip_address` without `port_id` | `400` with `BadRequest`. |
| Port without a fixed IPv4 address, or `fixed_ip_address` that is not the port's IPv4 address | `400` with `BadRequest`. |
| No router connects the port's subnet to a gateway on the external network | `404` with `ExternalGatewayForFloatingIPNotFound`. |
| The port's fixed IP already has a floating IP on the same external network | `409` with `FloatingIPPortAlreadyAssociated`. |
| External allocation pool is exhausted | `409` with `IpAddressGenerationFailure`. |
| `floating_ip_address` | `403` with `PolicyNotAuthorized`. The fixture user has the `member` role, and Neutron requires an administrator or project manager. |
| `floating_network_id`, `router_id`, or another read-only field in an update | `400` with `BadRequest`. |

A failed request allocates no address. See [Neutron L3 exceptions] and
[Neutron floating IP address policy].

### Status

Neutron stores a new floating IP as `DOWN`, and an L3 agent later reports
`ACTIVE` for associated addresses and `DOWN` for addresses it removes.
SandStack applies the agent's result immediately after each response. A
create response shows `DOWN`, and an update response shows the status from
before the update. Subsequent reads and lists show `ACTIVE` while the
floating IP is associated and `DOWN` otherwise. SandStack ignores the
router's administrative state and does not model the agent's `ERROR`
status. See [Neutron initial floating IP status] and [Neutron L3 agent
status reports].

### Filter and page through floating IPs

Filter by `id`, `floating_network_id`, `port_id`, `router_id`, `status`,
`fixed_ip_address`, or `floating_ip_address`. Matches are exact. A repeated
key matches any of its values, and different keys must all match.
Unassociated floating IPs never match `port_id`, `router_id`, or
`fixed_ip_address` filters. Other query parameters return `400`. In
Gophercloud, set the matching fields in `floatingips.ListOpts`. See the
[floating IP list reference].

Use `limit`, `marker`, and `page_reverse` as for networks. Results are
ordered by ascending floating IP ID, and `floatingips_links` carries
absolute `next` and `previous` links. An unknown marker returns `404`
with `FloatingIPNotFound`. See [Page through the network
list](network.md#page-through-the-network-list).

### Dependent resources

While a floating IP is associated through a router:

- Removing the router interface for the port's subnet returns `409` with
  `RouterInterfaceInUseByFloatingIP`. See [Neutron interface checks].
- Clearing or replacing the router's external gateway returns `409` with
  `RouterExternalGatewayInUseByFloatingIp`. See [Neutron gateway removal].
- Changing the port's fixed IP returns `400`. See [Neutron fixed IP guard].

Deleting the port disassociates its floating IPs. They remain allocated
with `null` association fields and `DOWN` status. See [Neutron port
disassociation]. [Reset the sandbox](reset.md) to remove all floating IPs.

### Omissions

SandStack does not accept `subnet_id` or `router_id` when it creates a
floating IP. Requests with either field return `400`, although Neutron
accepts them. Floating IP responses omit tags, timestamps, revision
numbers, DNS fields, QoS policies, and port details. Port forwarding and
packet forwarding are outside this scope.

## Verification

Run `go test ./...` to exercise allocation, association through the
router gateway, disassociation, filtering, pagination, error statuses,
and the effects on routers and ports. Gophercloud v2.15.0 tests cover the
client flow. HTTP tests use the [floating IP create reference],
[floating IP list reference], [floating IP details reference],
[floating IP update reference], and [floating IP delete reference].

[floating IP create reference]: https://docs.openstack.org/api-ref/network/v2/#create-floating-ip
[floating IP list reference]: https://docs.openstack.org/api-ref/network/v2/#list-floating-ips
[floating IP details reference]: https://docs.openstack.org/api-ref/network/v2/#show-floating-ip-details
[floating IP update reference]: https://docs.openstack.org/api-ref/network/v2/#update-floating-ip
[floating IP delete reference]: https://docs.openstack.org/api-ref/network/v2/#delete-floating-ip
[Neutron floating IP attributes]: https://github.com/openstack/neutron-lib/blob/5.0.0/neutron_lib/api/definitions/l3.py#L106-L148
[Neutron L3 exceptions]: https://github.com/openstack/neutron-lib/blob/5.0.0/neutron_lib/exceptions/l3.py#L43-L69
[Neutron floating IP creation]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L1514-L1649
[Neutron floating IP response]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L1256-L1283
[Neutron router selection]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L1294-L1343
[Neutron port association checks]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L1364-L1410
[Neutron association update]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L1446-L1509
[Neutron floating IP address policy]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/conf/policies/floatingip.py#L61-L77
[Neutron initial floating IP status]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/services/l3_router/l3_router_plugin.py#L186-L199
[Neutron L3 agent status reports]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/agent/l3/router_info.py#L1012-L1025
[Neutron interface checks]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L1113-L1140
[Neutron gateway removal]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L464-L498
[Neutron fixed IP guard]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L1816-L1829
[Neutron port disassociation]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/l3_db.py#L1872-L1944
