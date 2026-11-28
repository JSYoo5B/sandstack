# Security groups in the Networking sandbox

[Networking sandbox](network.md)

Authenticate with the [Identity fixture](identity.md) and use the
project-scoped token on the Neutron v2.0 endpoint.

## Discover the default security group

Use `groups.List` with `groups.ListOpts{Name: "default"}` and follow the
returned ID with `groups.Get` in Gophercloud v2.15.0. The Neutron v2.0
routes are `GET /security-groups` and
`GET /security-groups/{security_group_id}` under the same endpoint.
The project's default group is stateful and starts with four rules. Its
IPv4 and IPv6 ingress rules reference the group's own ID. The two egress
rules cover the same IP versions with no remote group. The rules have no
protocol, port range, or remote IP prefix restriction. See the [Neutron
default group] and [Neutron default rule templates].

List filters accept `id`, `name`, `project_id`, and `tenant_id`.
Repeated values for the same filter are alternatives. When both project
aliases are supplied, `project_id` takes precedence. A list with no
matches returns `200` with an empty `security_groups` array. A missing
group returns `404` on detail. The
[tag filters](network-tags.md#filter-lists-by-tag) also apply. See the
[Gophercloud group operations].

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

## Update a security group

Use `groups.Update` with `groups.UpdateOpts` to change a group's name,
description, or stateful mode. It sends
`PUT /security-groups/{security_group_id}` and returns `200` with the
updated `security_group` object. Omitted fields keep their values and the
rules are unchanged. A raw request can send an empty name, which
Gophercloud omits. Ownership fields and rules cannot be updated and return
`400`. See the [Neutron group update] and [Gophercloud group operations].

A new name of `default` in any case returns `409
SecurityGroupDefaultAlreadyExists`. The default group keeps its name and
returns `409 SecurityGroupCannotUpdateDefault` when a name is supplied, but
its description and mode can change. Changing the mode of a group bound to
a port returns `409 SecurityGroupInUse`. Sending the current mode
succeeds. A rejected update changes no field. SandStack does not expose
revision numbers, so `UpdateOpts.RevisionNumber` is not supported.

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

## Create and delete a security group rule

Use `rules.Create` on an owned group. Supply its ID, an ingress or egress
direction, and an IPv4 or IPv6 EtherType. The request returns `201` with a
`security_group_rule` object. Use the returned ID in standalone lookups or
read the rule through the group's detail. See the [Neutron rule creation]
and [Gophercloud rule create options].

The protocol can be TCP, UDP, ICMP, or IPv6 ICMP, by case-insensitive
name or by number (`6`, `17`, `1`, or `58`). Omit it, or send null or `any`,
to match any protocol. The response keeps the name or number as sent. An
IPv6 rule stores `icmp` and `icmpv6` as `ipv6-icmp` and `1` as `58`. IPv6
ICMP on an IPv4 rule returns `400`. Other protocols that Neutron accepts
are not supported and return `400`. See the [Neutron rule protocols] and
[Neutron IPv6 ICMP normalization].

Restrict the remote end with an address or CIDR of the rule's IP version,
or with `remote_group_id` naming a group in the same project. A
standalone address uses a host mask. Supplying both returns `400
SecurityGroupMultipleRemoteEntites`. A missing or foreign remote group
returns `404 SecurityGroupNotFound`. Deleting a group also deletes the
rules in other groups that use it as their remote group. See the [Neutron
remote validation] and [Neutron remote group cascade].

For TCP and UDP, supply both port endpoints in the range 1 to 65535. For
ICMP and IPv6 ICMP, `port_range_min` is the type and `port_range_max` is
the code, each from 0 to 255. A code without a type returns `400
SecurityGroupMissingIcmpType`. Ports without a protocol return `400`.
Omitting both bounds, or supplying 1 to 65535 for any protocol, stores
null bounds. See the [Neutron rule port ranges].

The response preserves host bits in `remote_ip_prefix` and exposes the
masked network in `normalized_cidr`. Both are null for a remote group
rule, and `remote_group_id` is null for an address rule. An existing rule
with the same selectors returns `409`, regardless of description. The
comparison treats a protocol name and its number as equal, and
`0.0.0.0/0` or `::/0` as no remote address. Use `rules.Delete` to remove
an owned rule, including rules in the default group. It returns `204`
with an empty body. Reset restores the default rules. See the [Neutron
rule deletion].

## Verification

Run `go test ./...` to exercise default discovery, custom group and
rule lifecycles, remote group and ICMP rules, group updates, and the
standalone and inline rule views with Gophercloud v2.15.0. Raw HTTP tests
cover project scoping, invalid selectors, protocol and ICMP validation,
CIDR normalization, duplicate detection, and deletion responses.

[Neutron group update]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L303-L344
[Neutron default group]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L166-L231
[Neutron default rule templates]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/migration/alembic_migrations/versions/2023.2/expand/c33da356b165_security_group_default_rules.py#L35-L78
[Gophercloud group operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/networking/v2/extensions/security/groups/requests.go
[Neutron custom group creation tests]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/tests/unit/extensions/test_securitygroup.py#L338-L377
[Neutron group name tests]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/tests/unit/extensions/test_securitygroup.py#L515-L545
[Neutron group deletion]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L261-L300
[Neutron rule lookups]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L1046-L1099
[Gophercloud rule operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/networking/v2/extensions/security/rules/requests.go#L19-L64
[Neutron rule creation]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L965-L1032
[Neutron rule port ranges]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L770-L819
[Neutron rule deletion]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L1101-L1120
[Gophercloud rule create options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/networking/v2/extensions/security/rules/requests.go#L109-L175
[Neutron rule protocols]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/extensions/securitygroup.py#L151-L167
[Neutron IPv6 ICMP normalization]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L821-L888
[Neutron remote validation]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L890-L913
[Neutron remote group cascade]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/models/securitygroup.py#L73-L101
