# Neutron subnets

Use the [Networking sandbox](network.md) to discover the endpoint,
authenticate, and create the network for these subnet operations.

## Add an IPv4 subnet

The subnet collection supports `POST` and `GET` at
`/network/v2.0/subnets`. Use `GET` or `DELETE` on that path followed by the
subnet ID to read or remove a subnet. Gophercloud provides `subnets.Create`,
`subnets.List`, `subnets.Get`, and `subnets.Delete` for these requests.

Create a subnet on a network owned by the token's project. Send `network_id`,
`ip_version: 4`, and an IPv4 `cidr` with a prefix length from 1 through 30.
The optional name defaults to an empty string and `enable_dhcp` defaults to
`true`. SandStack normalizes the prefix and uses the first usable address
as the default gateway. Overlapping subnets on one network receive `400`.

Set `dns_nameservers` to an ordered array of up to five distinct IPv4
addresses when creating a subnet. The default is an empty array. With
Gophercloud v2.15.0, set `subnets.CreateOpts.DNSNameservers` and read the
field on each `Subnet` returned by `subnets.Get` or `subnets.List`. See
the [subnet create reference] and [Gophercloud subnet options].

Network reads include the IDs of their subnets. Deleting a subnet removes
its ID from the network. Deleting a network with no ports also deletes its subnets.
A sandbox reset clears created subnets.

### Choose an IPv4 subnet gateway

Set `gateway_ip` at creation to choose the subnet gateway. Omitting it
selects the first usable address. Send JSON `null` to create a subnet
with no gateway. In Gophercloud v2.15.0, set `subnets.CreateOpts.GatewayIP`
to a pointer to the address, or leave it nil for the default. A pointer
to an empty string sends JSON `null`. See the [subnet create reference] and
[Gophercloud gateway encoding].

An IPv4 gateway within the subnet is excluded from the default allocation
pools. An interior gateway splits the usable range into two pools. A
gateway outside the subnet is accepted, and the entire usable range is
available by default, as it is when there is no gateway. An IPv6 address
or the subnet's own network or broadcast address returns `400`. A custom
pool containing the gateway returns `409`. See the [Neutron gateway
validation], [Neutron gateway pool generation], and [Neutron allocation
pool gateway check].

SandStack uses this gateway as subnet metadata and to allocate an address
for a router interface attached by subnet ID.

Use `PUT /network/v2.0/subnets/{subnet_id}` with `gateway_ip` in the
`subnet` object to change the gateway. Use `subnets.UpdateOpts.GatewayIP`
with `subnets.Update`. A nil pointer omits the field. A pointer to an
empty string sends JSON `null`, which clears the gateway. A gateway-only
update preserves the existing allocation pools and port IPs. A later router
attachment by subnet ID uses the new gateway when it is within the subnet.
See the [subnet update reference], [Gophercloud gateway update encoding],
and [Neutron gateway update].

Changing or clearing a gateway currently assigned to a router interface
returns `409` with `GatewayIpInUse`. Sending the same gateway succeeds.
A new gateway within a pool that applies after the update returns `409`
with `GatewayConflictWithAllocationPools`. See the [Neutron gateway
validation] and [Neutron gateway update].

### Set IPv4 allocation pools

At subnet creation, send `allocation_pools` with one or more `start`
and `end` pairs. Set the ranges with `subnets.CreateOpts.AllocationPools`
in Gophercloud v2.15.0. Omit the field or send `[]` or `null` to derive
the default pool. For `192.0.2.0/24` with gateway `192.0.2.1`, it spans
`192.0.2.2` through `192.0.2.254`. See the [subnet create reference],
[Gophercloud subnet options], and [Neutron allocation pool defaults].

Both endpoints are inclusive. A malformed, reversed, or out-of-bounds
range returns `400`, including any range that contains the network or
broadcast address. A range that contains the gateway returns `409`.
Ranges that share any address, including an endpoint, return `409`
with `OverlappingAllocationPools`. Adjacent ranges are valid and stay
separate. See the [Neutron allocation pool bounds] and [Neutron allocation
pool gateway check].

The fixture returns pools in ascending order by start address. Automatic
allocation chooses the lowest free address across all pools and skips
gaps. An explicit `fixed_ips` request can select a valid, free IPv4
address in a gap. See the [Neutron IP allocation validation] and
[Neutron automatic allocation].

To replace the pools, send `allocation_pools` in a subnet `PUT`. Omit the
field to preserve the existing pools. In Gophercloud v2.15.0, set
`subnets.UpdateOpts.AllocationPools` to a nonempty slice. To clear the
pools, send `[]` or `null` in a raw request. The SDK omits an empty slice.
After clearing, automatic allocation returns `409`. An explicit request
can still claim a valid, free IPv4 address in the subnet. Existing port
IPs stay allocated, including those outside the replacement ranges.
See the [subnet update reference], [Neutron pool replacement], and
[Neutron pool input conversion].

You can update `gateway_ip` and `allocation_pools` in the same request.
SandStack checks the resulting gateway against the resulting pools and
applies both changes together. If validation fails, the subnet retains
its previous configuration. See the [Neutron gateway update].

### Filter the subnet list

`GET /network/v2.0/subnets` lists subnets in the token's project. Filter
by `id`, `name`, `network_id`, `cidr`, `gateway_ip`, or `enable_dhcp`. A
repeated key matches any of its values. Different keys must all match,
and an omitted key leaves that field unrestricted. Supply CIDRs as an
address and prefix length. For a subnet with CIDR `192.0.2.0/24`,
`192.0.2.0/24` matches and
`192.0.2.10/24` returns no match. SandStack returns `400` for unsupported
query keys and invalid CIDR or DHCP filter values. See the
[subnet list reference], [Neutron subnet schema], [Neutron filter parser],
and [Neutron query filters].

The `id` and `gateway_ip` filters match exact values. Gateway updates
take effect in the next list request. Malformed IDs and addresses that
decode successfully return no matches, as do empty values and the
literal `null`. To include subnets with no gateway, omit `gateway_ip`.

In Gophercloud v2.15.0, pass `ID`, `Name`, `NetworkID`, `CIDR`, `GatewayIP`,
or `EnableDHCP` in `subnets.ListOpts` to `subnets.List`. Set `EnableDHCP`
to a pointer to `false` to select subnets with DHCP disabled. Leave the
pointer nil to include both DHCP states. See the [Gophercloud subnet options].

### Update subnet metadata

Send `PUT /network/v2.0/subnets/{subnet_id}` with a `subnet` object to
update `name`, `enable_dhcp`, `dns_nameservers`, `host_routes`, `gateway_ip`,
or `allocation_pools` in the token's project. A successful request returns `200`
with the updated subnet. Omitted fields keep their values. Use an empty
string to clear the name or `false` to disable the DHCP flag. Pass `Name` and
`EnableDHCP` pointers in `subnets.UpdateOpts` to `subnets.Update`.
See the [subnet update reference].

To replace the DNS nameservers, pass a pointer to the new slice in
`subnets.UpdateOpts.DNSNameservers`. Pass a pointer to an empty slice to
send `dns_nameservers: []` and clear the list. A nil pointer omits the
field and preserves the list. See the [Gophercloud subnet options].
A raw HTTP request can also send `null` to clear the list.
Invalid, duplicate, or excess entries return `400` and leave the subnet
unchanged. The limit matches the [Neutron DNS nameserver default].

SandStack records the DHCP flag and DNS nameservers as metadata. The
subnet's existing IPv4 allocations and router attachment remain in place.
The CIDR is set when the subnet is created.

### Configure IPv4 host routes

Set `host_routes` when creating a subnet or replace all host routes in a
subnet update. Omit the field during an update to preserve the list, or
pass `[]` or `null` to clear it. Subnet detail and list responses include
`host_routes`, which is empty by default. In Gophercloud v2.15.0, use
`subnets.CreateOpts.HostRoutes` on create and `subnets.UpdateOpts.HostRoutes`
on update. Pass a pointer to an empty slice to clear the list. Read the
routes from `Subnet.HostRoutes`. See the [Neutron host route schema] and
[Gophercloud subnet options].

Each entry has exactly two fields, `destination` and `nexthop`. Use a
canonical IPv4 destination prefix with host bits set to zero and an IPv4
next-hop address. Loopback destinations are invalid. A next hop may be
outside the subnet. A destination may appear more than once with different
next hops. SandStack accepts up to 20 distinct destination and next-hop
pairs. Invalid entries, duplicate pairs, and excess routes return `400`.
Compare routes as a set because response order is unspecified. SandStack
stores these routes as subnet metadata. See the [Neutron host route
validation], [Neutron host route limit], and [Neutron subnet IP validation].

## Verification

Run `go test ./...` to check subnet lifecycle, filters, allocation pools,
and gateway changes through Gophercloud. The raw HTTP tests use the
[subnet create reference], [subnet list reference], and [subnet update
reference] to check request and response behavior.

Subnet tests verify DNS nameserver order, replacement, clearing, omission,
and validation against the [subnet update reference], [Neutron subnet schema],
and [Neutron nameserver validation].
The subnet host route scenario checks creation and reads, replacement,
omission, clearing, and invalid updates against the [Neutron host route
schema] and [Neutron host route validation].
Gateway update tests check pool and port address preservation, conflicts,
and attachment with the updated gateway.

[subnet create reference]: https://docs.openstack.org/api-ref/network/v2/#create-subnet
[Gophercloud gateway encoding]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/networking/v2/subnets/requests.go#L158-L169
[Gophercloud gateway update encoding]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/networking/v2/subnets/requests.go#L234-L244
[Neutron gateway validation]: https://github.com/openstack/neutron/blob/master/neutron/db/db_base_plugin_v2.py#L687-L718
[Neutron gateway update]: https://github.com/openstack/neutron/blob/master/neutron/db/db_base_plugin_v2.py#L943-L994
[Neutron gateway pool generation]: https://github.com/openstack/neutron/blob/master/neutron/ipam/utils.py#L33-L73
[Neutron allocation pool defaults]: https://github.com/openstack/neutron/blob/master/neutron/ipam/drivers/neutrondb_ipam/driver.py#L56-L77
[Neutron pool replacement]: https://github.com/openstack/neutron/blob/master/neutron/ipam/drivers/neutrondb_ipam/driver.py#L266-L275
[Neutron pool input conversion]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/api/converters.py#L169-L188
[Neutron allocation pool bounds]: https://github.com/openstack/neutron/blob/master/neutron/db/ipam_backend_mixin.py#L270-L329
[Neutron allocation pool gateway check]: https://github.com/openstack/neutron/blob/master/neutron/db/ipam_backend_mixin.py#L415-L430
[Neutron automatic allocation]: https://github.com/openstack/neutron/blob/master/neutron/ipam/drivers/neutrondb_ipam/driver.py#L143-L187
[subnet list reference]: https://docs.openstack.org/api-ref/network/v2/#list-subnets
[subnet update reference]: https://docs.openstack.org/api-ref/network/v2/#update-subnet
[Neutron filter parser]: https://github.com/openstack/neutron/blob/master/neutron/api/api_common.py#L65-L107
[Neutron query filters]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/db/model_query.py#L198-L236
[Neutron subnet schema]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/api/definitions/subnet.py
[Neutron host route schema]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/api/definitions/subnet.py#L103-L108
[Neutron host route validation]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/api/validators/__init__.py#L747-L777
[Neutron subnet IP validation]: https://github.com/openstack/neutron/blob/master/neutron/db/db_base_plugin_v2.py#L731-L739
[Neutron host route limit]: https://github.com/openstack/neutron/blob/master/neutron/conf/common.py#L63-L66
[Neutron nameserver validation]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/api/validators/__init__.py#L716-L745
[Neutron DNS nameserver default]: https://docs.openstack.org/neutron/2026.1/configuration/neutron.html#DEFAULT.max_dns_nameservers
[Gophercloud subnet options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/networking/v2/subnets/requests.go
[Neutron IP allocation validation]: https://github.com/openstack/neutron/blob/master/neutron/ipam/drivers/neutrondb_ipam/driver.py#L125-L141
