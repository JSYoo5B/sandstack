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
| `PUT /network/v2.0/networks/{network_id}` | Updates a network owned by the token's project and returns `200`. Use `networks.Update` in Gophercloud. |
| `DELETE /network/v2.0/networks/{network_id}` | Removes a network owned by the token's project and returns `204`. Use `networks.Delete` in Gophercloud. |

Use project-scoped `X-Auth-Token` credentials. All networks are private and
start with no subnets. [Reset the sandbox](reset.md) to restore the seed and
remove networks, subnets, and ports created by the client.

## Discover the default security group

Use `groups.List` with `groups.ListOpts{Name: "default"}` and follow the
returned ID with `groups.Get` in Gophercloud v2.15.0. The Neutron v2.0
routes are `GET /security-groups` and
`GET /security-groups/{security_group_id}` under the same endpoint.
The project's default group is stateful and contains four rules. Its
IPv4 and IPv6 ingress rules reference the group's own ID. The two egress
rules cover the same IP versions with no remote group. The rules have no
protocol, port range, or remote IP prefix restriction. See the [Neutron
default group] and [Neutron default rule templates].

List filters accept `id`, `name`, `project_id`, and `tenant_id`.
Repeated values for the same filter are alternatives. When both project
aliases are supplied, `project_id` takes precedence. A list with no
matches returns `200` with an empty `security_groups` array. A missing
group returns `404` on detail. See the [Gophercloud group operations].

## Create and delete a security group

Use `groups.Create` with a name and optional description. The project
token determines ownership. The request returns `201` with a
`security_group` object containing one unrestricted IPv4 egress rule
and one unrestricted IPv6 egress rule. The group is stateful by default.
Pass a pointer to `false` in `groups.CreateOpts.Stateful` for a stateless
group. A raw `security_group` object can omit the name and description,
which default to empty strings. See the [Neutron custom group creation
tests] and [Gophercloud group operations].

Ordinary names can repeat and each create receives a distinct ID.
The name `default` is reserved regardless of case and returns `409`.
Use `groups.Delete` to remove a custom group and its rules. It returns
`204` with an empty body. Group detail returns `404` after deletion.
Deleting the project's default group with the project token returns
`409`. See the [Neutron group name tests] and [Neutron group deletion].

## Read security group rules

Use `rules.List` with `rules.ListOpts{SecGroupID: groupID}` and follow a
rule ID with `rules.Get`. The Neutron v2.0 routes are
`GET /security-group-rules` and `GET /security-group-rules/{rule_id}`.
They return `200` and include the same rule fields as group detail. These
standalone reads also expose `belongs_to_default_sg`. A missing rule
returns `404`. Deleting a custom group removes its rules from both
standalone reads. See the [Neutron rule lookups] and [Gophercloud rule operations].

Filter the list by ID, group ID, direction, EtherType, or either project
alias. Repeated values within a filter are alternatives and different
filters apply together. EtherType accepts IPv4 or IPv6 regardless of
case. An invalid EtherType returns `400`. A list with no matches returns
an empty `security_group_rules` array.

## Create a network

Send a `network` object with `name` and `admin_state_up` as needed. A name
may be empty or contain up to 255 printable characters with no surrounding
whitespace. The administrative state defaults to up. You can also pass
`shared: false` and set `project_id` or `tenant_id` to the token's project.
A network created with `admin_state_up: false` reports `DOWN`.

## Update network metadata

Send `PUT /network/v2.0/networks/{network_id}` with a `network` object
to change `name` or `admin_state_up`. Omit a field to keep its value or
send an empty object for a no-op. An empty string clears the name, and
`false` disables the network administratively. With Gophercloud v2.15.0,
pass `Name` or `AdminStateUp` pointers in `networks.UpdateOpts` to
`networks.Update`. Nil pointers omit their fields. See the [Neutron
update reference] and [Gophercloud network options].

The network reports `DOWN` when it is administratively disabled or has
an injected outage. Re-enabling it during an outage keeps the status
`DOWN`. Clearing an outage while the network is disabled also keeps the
status `DOWN`. The network reports `ACTIVE` once it is enabled and the
outage is clear.

## Filter the network list

Filter by `name`, `status`, or `admin_state_up`. Name and status matches
are exact. A repeated key matches any of its values, and different keys
must all match. For example,
`?status=ACTIVE&status=DOWN` selects either status. Omit a key to leave it
unrestricted. An explicit `name=` matches unnamed networks, and `status=`
matches no networks. A list without matches returns `{"networks":[]}`.
See the [Neutron list reference] and [Neutron network filter schema].

`?status=DOWN&admin_state_up=false` selects administratively disabled
networks, including any that also have an injected outage. Use
`?status=DOWN&admin_state_up=true` to find enabled networks with an
injected outage. With Gophercloud v2.15.0, set `Name`, `Status`, or
`AdminStateUp` in `networks.ListOpts`. Pass a pointer to `false` to
select disabled networks. A nil pointer omits the filter.

The list also accepts the pagination parameters below. SandStack returns
`400` for invalid boolean filter values, unsupported list parameters, and
query parameters on network detail, creation, update, and deletion.

## Page through the network list

Use `limit` for the page size and `marker` for the network ID after which
the next page begins. Results are ordered by ascending network ID. With
Gophercloud v2.15.0, pass `Limit` and optionally `Marker` in
`networks.ListOpts`. The SDK follows the absolute `next` URL in
`networks_links`. Omit `limit` or set it to zero to return all matches and
ignore the marker. A negative or noninteger limit returns `400`.

A marker can identify a network in the token's project even when the
current filters exclude that network. A paged request returns `404`
when its marker identifies an unknown network or a network in another
project. See the [Neutron network pagination] and [Neutron network
marker lookup].

Follow the `previous` URL with an authenticated HTTP GET to read the
preceding page. It uses the first network ID on the current page as the
marker and sets `page_reverse=True`. With a positive limit and no marker,
`page_reverse=True` starts at the last page. Reverse requests still show
results in ascending ID order. Both links retain the filters and page
size. SandStack includes each link when matching networks remain in that
direction. See the [Neutron pagination reference].

## Resource guides

### Add an IPv4 subnet

Use the [subnet guide](network-subnets.md#add-an-ipv4-subnet) for gateways,
allocation pools, filters, DNS nameservers, and host routes.

### Allocate and release a port address

Use the [port guide](network-ports.md#allocate-and-release-a-port-address)
for automatic and explicit allocation, updates, and fixed IP searches.

### Manage a router

Use the [router guide](network-routers.md#manage-a-router) for router
metadata, list filters, and static routes.

### Attach and remove an IPv4 router interface

Use the [router interface guide](network-routers.md#attach-and-remove-an-ipv4-router-interface)
to attach by subnet or existing port and inspect the resulting interface.

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

Run `go test ./...` to exercise discovery, project-scoped network
lifecycle, filtered pagination, and the outage controls through Gophercloud.
The HTTP tests use the [Neutron filtering reference], [Neutron list reference],
[Neutron detail reference], [Neutron create reference], and
[Neutron delete reference]. Network update tests check partial updates and
how administrative changes and outages affect later reads. See the
[Neutron update reference].
The security group tests exercise SDK discovery, custom creation and
deletion, rule defaults, and project scoping over HTTP.

[Neutron filtering reference]: https://docs.openstack.org/api-ref/network/v2/#filtering-and-column-selection
[Neutron list reference]: https://docs.openstack.org/api-ref/network/v2/#list-networks
[Neutron network filter schema]: https://github.com/openstack/neutron-lib/blob/master/neutron_lib/api/definitions/network.py#L48-L61
[Neutron detail reference]: https://docs.openstack.org/api-ref/network/v2/#show-network-details
[Neutron create reference]: https://docs.openstack.org/api-ref/network/v2/#create-network
[Neutron update reference]: https://docs.openstack.org/api-ref/network/v2/#update-network
[Neutron delete reference]: https://docs.openstack.org/api-ref/network/v2/#delete-network
[Gophercloud network options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/networking/v2/networks/requests.go#L119-L143
[Neutron pagination reference]: https://docs.openstack.org/api-ref/network/v2/#pagination
[Neutron network pagination]: https://github.com/openstack/neutron/blob/master/neutron/db/db_base_plugin_v2.py#L590-L603
[Neutron network marker lookup]: https://github.com/openstack/neutron/blob/master/neutron/db/db_base_plugin_common.py#L250-L255
[Neutron default group]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L166-L231
[Neutron default rule templates]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/migration/alembic_migrations/versions/2023.2/expand/c33da356b165_security_group_default_rules.py#L35-L78
[Gophercloud group operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/networking/v2/extensions/security/groups/requests.go
[Neutron custom group creation tests]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/tests/unit/extensions/test_securitygroup.py#L338-L377
[Neutron group name tests]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/tests/unit/extensions/test_securitygroup.py#L515-L545
[Neutron group deletion]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L261-L300
[Neutron rule lookups]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L1046-L1099
[Gophercloud rule operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/networking/v2/extensions/security/rules/requests.go#L19-L64
