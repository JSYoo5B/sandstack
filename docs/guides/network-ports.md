# Neutron ports

Use the [Networking sandbox](network.md) to authenticate and create a
private network. [Add an IPv4 subnet](network-subnets.md#add-an-ipv4-subnet)
before testing address allocation. The client uses the network's ID and
supplies a subnet ID when choosing a specific allocation pool.
To connect an existing port to a router, follow the
[router interface guide](network-routers.md#attach-and-remove-an-ipv4-router-interface).

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

### Filter ports by fixed IP

Use `ports.ListOpts.FixedIPs` with `ports.FixedIPOpts.IPAddress` or
`SubnetID` to find ports with an exact address or subnet match. Gophercloud
sends each condition as a separate `fixed_ips` query value. For example,
`fixed_ips=ip_address%3D192.0.2.10&fixed_ips=subnet_id%3DSUBNET_ID` matches
ports that satisfy both conditions. Repeating an inner key matches any of
its values. These conditions combine with any `network_id`, `device_id`,
and `device_owner` filters in the same request. See the [port list
reference], [Neutron fixed IP filter parser], and [Neutron fixed IP filter
query].

Ports without a fixed IP are excluded when either fixed IP filter is
present. Exact values are matched as strings, so an unmatched literal
returns `200` with `ports: []`. Malformed entries and unsupported inner
keys return `400`, including `ip_address_substr` from Neutron's optional
`ip-substring-filtering` extension.

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

## Verification

Run `go test ./internal/api/server -run 'Test.*Port' -count=1` to exercise
port operations against the SandStack HTTP handler. Gophercloud scenarios
check allocation, release and reuse, updates, and filters applied before
and after changing addresses. Raw HTTP tests cover request validation,
response fields, and error codes for address and deletion conflicts.
The [Neutron database implementation] and [exception types] describe pool
exhaustion and resource-in-use errors.

[Neutron filtering reference]: https://docs.openstack.org/api-ref/network/v2/#filtering-and-column-selection
[port create reference]: https://docs.openstack.org/api-ref/network/v2/#create-port
[port list reference]: https://docs.openstack.org/api-ref/network/v2/#list-ports
[port update reference]: https://docs.openstack.org/api-ref/network/v2/#update-port
[Neutron fixed IP filter parser]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/api/converters.py#L110-L137
[Neutron fixed IP filter query]: https://github.com/openstack/neutron/blob/master/neutron/db/db_base_plugin_v2.py#L1552-L1583
[Neutron IP allocation validation]: https://github.com/openstack/neutron/blob/master/neutron/ipam/drivers/neutrondb_ipam/driver.py#L125-L141
[Neutron IP allocation update]: https://github.com/openstack/neutron/blob/master/neutron/db/ipam_pluggable_backend.py#L439-L506
[Neutron database implementation]: https://github.com/openstack/neutron/blob/master/neutron/db/db_base_plugin_v2.py
[exception types]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/exceptions/__init__.py
