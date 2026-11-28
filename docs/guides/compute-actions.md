# Server action history in the Compute sandbox

[Compute sandbox](compute.md)

Use the Nova v2.1 action history to find the record of a server action a
client started. Each record names the action and carries the request ID
that Nova returned with the action response.

## Match an action to its request ID

Every Compute response carries a request ID in `X-Openstack-Request-Id`
and the same value in `X-Compute-Request-Id`. The value is `req-` followed
by a random UUID. Each request receives a new ID, including requests that
fail. SandStack ignores an inbound `X-Openstack-Request-Id`. In
Gophercloud v2.15.0, read the header from the action result, such as
`servers.Stop(...).Header`. See the [Nova request ID middleware],
[oslo.middleware request ID], and [oslo.context request ID].

## List and read server actions

Use `instanceactions.List` and `instanceactions.Get` in Gophercloud
v2.15.0. They send `GET /servers/{server_id}/os-instance-actions` and
`GET /servers/{server_id}/os-instance-actions/{request_id}`. The list
returns `200` with an `instanceActions` array, newest first. Show returns
`200` with an `instanceAction` object for the newest record with that
request ID. See the [Nova instance action reference], [Nova instance
action controller], [Nova action ordering], and [Gophercloud instance
action operations].

Each record has `action`, `instance_uuid`, `request_id`, `project_id`,
`user_id`, `start_time`, and `message`. The start time is UTC with six
fractional digits and no zone designator, such as
`2026-11-25T01:02:03.000000`. Gophercloud decodes this layout. SandStack
completes actions synchronously, so `message` is always `null`. See the
[Nova action fields] and [Gophercloud action decoding].

At v2.1, Nova returns events only to administrators. The fixture project
is not an administrator, so show omits `events`. Version 2.1 accepts and
ignores list and show query parameters such as `limit`. See the [Nova
action event policy] and [Nova action query schema].

An unknown server returns `404`. An unknown request ID, including the ID
of a rejected request, returns `404` for show.

## Recorded actions

A successful request records one of these actions:

| Request | Action |
| --- | --- |
| `os-stop` | `stop` |
| `os-start` | `start` |
| `reboot`, soft or hard | `reboot` |
| `pause` and `unpause` | `pause` and `unpause` |
| `suspend` and `resume` | `suspend` and `resume` |
| `lock` and `unlock` | `lock` and `unlock` |
| Volume attachment create | `attach_volume` |
| Volume attachment delete | `detach_volume` |

Nova checks the lock, server state, and volume state before it records an
action, so a request that returns `400`, `404`, or `409` adds no record.
Locking a server that the owner already locked returns `202` without a
record. Unlock records every accepted request. Rename and metadata writes
do not record actions. The start time comes from the sandbox clock when
the action is accepted. See the [Nova action recording], [Nova power
action recording], [Nova lock recording], [Nova attach recording], and
[Nova detach recording].

## Fixture history

Nova records a `create` action when it builds a server. The fixture
server lists one `create` record with the fixture project and user, the
server's creation time, and a request ID generated at startup. That ID
stays the same for the sandbox's lifetime. See the [Nova create
recording].

[Reset the sandbox](reset.md) to remove recorded actions. The `create`
record remains.

## Verification

The SDK test reads the request ID from stop, attach, and lock responses
and finds each record with list and show. It also checks that a `409`
pause and a repeated lock add no record, that an unknown request ID
returns `404`, and that reset restores the `create` record. Raw HTTP
checks the request ID headers, the seven record fields, the start time
layout, the absent `events` field, and `404` responses against the pinned
Nova source.

[Nova request ID middleware]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/compute_req_id.py#L32-L36
[oslo.middleware request ID]: https://github.com/openstack/oslo.middleware/blob/8.3.0/oslo_middleware/request_id.py#L61-L77
[oslo.context request ID]: https://github.com/openstack/oslo.context/blob/6.6.0/oslo_context/context.py#L67-L69
[Nova instance action reference]: https://docs.openstack.org/api-ref/compute/#servers-actions-servers-os-instance-actions
[Nova instance action controller]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/instance_actions.py#L82-L215
[Nova action ordering]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/db/main/api.py#L3896-L3937
[Gophercloud instance action operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/instanceactions/request.go#L59-L81
[Nova action fields]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/objects/instance_action.py#L62-L70
[Gophercloud action decoding]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/instanceactions/results.go#L12-L51
[Nova action event policy]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/policies/instance_actions.py#L60-L77
[Nova action query schema]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/instance_actions.py#L20-L50
[Nova action recording]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L476-L478
[Nova power action recording]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2870-L2899
[Nova lock recording]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5046-L5098
[Nova attach recording]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/conductor/manager.py#L2383-L2391
[Nova detach recording]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5313-L5338
[Nova create recording]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/conductor/manager.py#L1811-L1819
