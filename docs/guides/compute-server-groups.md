# Server groups in the Compute sandbox

[Compute sandbox](compute.md)

Use the project token from the [Identity fixture](identity.md) to manage
the project's server groups with Nova v2.1 and to boot
[servers](compute-servers.md) into them. A server group belongs to the
project that created it.

## Create a server group

Use `servergroups.Create` in Gophercloud v2.15.0 with
`servergroups.CreateOpts{Name: "web", Policies: []string{"anti-affinity"}}`.
It sends `POST /os-server-groups` with a `server_group` object. Nova v2.1
returns `200` with `id`, `name`, `policies`, `members`, and `metadata`.
`members` starts as `[]` and `metadata` is always `{}`. See the [Nova
server group reference], [Nova server group create], [Nova server group
view], and [Gophercloud server group operations].

`name` is required and follows the [server name rules](compute.md#rename-a-server).
`policies` is required and holds exactly one of `affinity` or
`anti-affinity`. Nova accepts `soft-affinity` and `soft-anti-affinity`
from microversion 2.15 and replaces `policies` with `policy` and `rules`
at 2.64, so at v2.1 those policies and fields return `400`. Leave
`CreateOpts.Policy` and `CreateOpts.Rules` empty. A missing name, a
missing or empty `policies` array, more than one policy, an unknown policy,
and any other field return `400`. See the [Nova server group schema].

Each project can hold Nova's default quota of 10 server groups. Creating
one more returns `403` with `Quota exceeded, too many server groups.` See
the [Nova server group quota].

## List, read, and delete server groups

Use `servergroups.List`, `servergroups.Get`, and `servergroups.Delete`
with `GET /os-server-groups`, `GET /os-server-groups/{id}`, and
`DELETE /os-server-groups/{id}`. List and read return `200` with the same
fields as the create response. The list wraps them in `server_groups` in
creation order. Delete returns `204` with an empty body and leaves the
member servers in place. See the [Nova server group read and delete] and
[Nova server group list].

The list accepts `offset` and `limit` as non-negative integers and ignores
other query keys, as v2.1 does. `limit` `0` or above 1000 returns up to
1000 groups, Nova's default `max_limit`. A negative or non-numeric value
returns `400`. See the [Nova server group list query] and [Nova list
limits].

`all_projects` lists every project's groups only for an administrator.
Nova returns the caller's own groups to anyone else. SandStack has no
administrator role for Compute requests, so the list always contains the
project's groups. See the [Nova server group policies].

An unknown ID, including a group of another project, returns `404` with
`Instance group {id} could not be found.` Nova looks groups up within the
caller's project. See the [Nova server group lookup] and [Nova group not
found].

## Add a server to a group

Pass `servers.SchedulerHintOpts{Group: groupID}` as the hint options of
`servers.Create`. Gophercloud sends the group ID as
`"os:scheduler_hints": {"group": "..."}` beside the `server` object. Nova
also reads `OS-SCH-HNT:scheduler_hints` when `os:scheduler_hints` is
absent. The server joins the group's `members` when Nova accepts the
request, and leaves it when the server is deleted. See the [Nova
scheduler hints], [Nova scheduler hint schema], [Nova requested group],
and [Gophercloud scheduler hints].

The group must be a UUID. Other values return `400`. An unknown group,
including a group of another project, returns `400` with
`Instance group {id} could not be found.` Nova looks up the group after
the image status checks and before the block device volume checks. See
the [Nova create errors].

Each group can hold Nova's default quota of 10 servers per user. Adding
one more returns `403` with `Quota exceeded, too many servers in group`.
Nova checks the quota after the block device volume checks. See the [Nova
group member quota] and [Nova over quota].

## Scheduling on one host

SandStack models a single compute host. An `affinity` group therefore
accepts every server. In an `anti-affinity` group, a server boots only
when no other member runs on the host. Otherwise Nova's scheduler finds no
valid host. The server joins the group and reports `ERROR` with a code
`500` fault and the message `No valid host was found. There are not
enough hosts available.` It has no ports, because Nova creates them on
the compute host. A root volume stays reserved as after
[`fail-create`](compute-servers.md#root-volume). Delete it, or delete the other member, before booting
another member. See the [Nova anti-affinity filter] and [Nova scheduler
failure].

A member runs on the host once its build completes as `ACTIVE`. A member
whose build fails or that never left `BUILD` does not occupy the host,
because Nova clears the host of a failed build. See the [Nova build
failure cleanup]. With [held builds](compute-servers.md#wait-for-the-server),
SandStack schedules each server when `complete-create` runs. If another
member has completed by then, `complete-create` returns `204` and fails
the build in the same way. `fail-create` works as for any server.

## Omissions

SandStack accepts only the `group` scheduler hint, as one UUID string.
Other hints, such as `same_host` or `different_host`, and a list of group
IDs return `400`. Nova's default scheduler filters use only the group
hint, and Nova's handling of a list of group IDs is not specified. See
the [Nova default filters]. Rebuild, resize, and power actions ignore the
group policy. Reset removes all server groups.

## Verification

Run `go test ./...` to check group creation, listing, reading, deletion,
member updates, and reset through Gophercloud. The Gophercloud test also
boots two `anti-affinity` members and checks the scheduling fault and the
missing ports. Raw HTTP tests check the v2.1 field sets, the `200` and
`204` status codes, request validation, list paging, both quotas, and the
`400` and `404` messages for unknown groups against the pinned Nova
source. Unit tests check project scoping, the check order, and held
builds that complete into an occupied host. No reference deployment was
observed.

[Nova server group reference]: https://docs.openstack.org/api-ref/compute/#create-server-group
[Nova server group create]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/server_groups.py#L208-L283
[Nova server group view]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/server_groups.py#L91-L131
[Gophercloud server group operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servergroups/requests.go#L11-L102
[Nova server group schema]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/server_groups.py#L22-L77
[Nova server group quota]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/conf/quota.py#L157-L172
[Nova server group read and delete]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/server_groups.py#L133-L166
[Nova server group list]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/server_groups.py#L168-L206
[Nova server group list query]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/server_groups.py#L79-L91
[Nova list limits]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L176-L231
[Nova server group policies]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/policies/server_groups.py#L49-L72
[Nova server group lookup]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/objects/instance_group.py#L42-L50
[Nova group not found]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/exception.py#L1646-L1647
[Nova scheduler hints]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L747-L752
[Nova scheduler hint schema]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L87-L151
[Nova requested group]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L1667-L1678
[Gophercloud scheduler hints]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L139-L279
[Nova create errors]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L890-L902
[Nova group member quota]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L1577-L1626
[Nova over quota]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L835-L837
[Nova anti-affinity filter]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/scheduler/filters/affinity_filter.py#L82-L135
[Nova scheduler failure]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/scheduler/manager.py#L542-L572
[Nova build failure cleanup]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L2594-L2610
[Nova default filters]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/conf/scheduler.py#L323-L333
