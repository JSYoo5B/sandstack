# Neutron resource tags

Use the [Networking sandbox](network.md) to authenticate. SandStack
implements Neutron's `standard-attr-tag` extension for the networks,
subnets, ports, routers, security groups, and floating IPs that it
models. Behavior follows Neutron at
`ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d` with neutron-lib 5.0.0. See
the [Neutron tag controller] and [Neutron tag plugin].

## Manage tags on a resource

Replace `{resources}` with `networks`, `subnets`, `ports`, `routers`,
`security-groups`, or `floatingips`. In Gophercloud v2.15.0, pass the
same collection name as the `resourceType` argument to the
[Gophercloud attributestags] functions.

| Request | Gophercloud | Behavior |
| --- | --- | --- |
| `GET /network/v2.0/{resources}/{id}/tags` | `attributestags.List` | Returns `200` with `{"tags":[...]}`. |
| `PUT /network/v2.0/{resources}/{id}/tags` | `attributestags.ReplaceAll` | Replaces every tag with the `tags` list in the body. Returns `200` and echoes the request body. |
| `DELETE /network/v2.0/{resources}/{id}/tags` | `attributestags.DeleteAll` | Removes every tag. Returns `204`. |
| `GET /network/v2.0/{resources}/{id}/tags/{tag}` | `attributestags.Confirm` | Returns `204` when the tag is present, or `404 TagNotFound`. |
| `PUT /network/v2.0/{resources}/{id}/tags/{tag}` | `attributestags.Add` | Adds one tag. Returns `201` with the JSON body `null`. Adding a tag that is already present also returns `201`. |
| `DELETE /network/v2.0/{resources}/{id}/tags/{tag}` | `attributestags.Delete` | Removes one tag. Returns `204`, or `404 TagNotFound` when the tag is absent. |

`attributestags.Confirm` reports `false` for the `404` response. Show and
list responses for each resource include a `tags` array, which is `[]`
for an untagged resource. Neutron does not define an order for these
arrays. SandStack returns tags in ascending order. The tag endpoints
reject query parameters with `400`, and the `DELETE` requests reject a
request body with `400`.

## Tag values

A tag is any JSON string of at most 255 characters. The empty string and
strings containing commas are valid tags. A comma-containing tag cannot be
matched by a list filter, because the filters split values on commas.
A resource holds at most 50 distinct tags. See the [Neutron tag limits]
and [neutron-lib string validators].

The replacement body must be an object with a `tags` list. Each failure
below returns `400 InvalidInput` with the message
`Invalid input for operation: <detail>.`:

| Request | Detail |
| --- | --- |
| A missing or non-object body, or a body without `tags` | `Invalid tags body` |
| A `tags` value that is not a list | `'<value>' is not a list` |
| A repeated tag | `Duplicate items in the list: '<tag>, <tag>'` |
| A list item that is not a string | `'<value>' is not a valid string` |
| A tag longer than 255 characters | `'<tag>' exceeds maximum length of 255` |

Neutron formats non-string values in Python notation, such as `None`,
`False`, `1.0`, or `['red']`. The duplicate check runs before the string
check. Single-tag requests check only the path tag's length. More than 50
distinct tags after a replacement or addition returns `400 BadRequest`
with `Bad <collection> request: The number of tags exceed the per-resource
limit of 50.`. The collection name uses underscores, such as
`security_groups`. Validation runs before the resource lookup, and the
tag limit is checked after it.

## Access and unknown resources

A project can read the tags of any resource it can show. That includes
the fixture external network and its subnet. Only the owning project can
change tags. Changing the external network or subnet tags returns
`403 PolicyNotAuthorized` with a message such as
`rule:update_network:tags is disallowed by policy`. `PUT` requests use
the `update` action and `DELETE` requests use the `delete` action. Reading
a resource that the project cannot see, such as a router gateway port,
returns the same error with the `get` action. See the
[Neutron network tag policy], [Neutron subnet tag policy], and the
[oslo.policy error].

At the pinned revision, Neutron's tag controller does not translate an
unknown resource ID into a not-found error. The lookup returns an empty
result, and building the policy action then fails. The API reports
`500 HTTPInternalServerError` with `Request Failed: internal server error
while processing your request.`. SandStack reproduces this response
for unknown IDs on every tag endpoint. See the [Neutron tag resource
lookup] and [Neutron exception translation].

## Filter lists by tag

Each of the six resource lists accepts these filters. A value can hold
several comma-separated tags, and a repeated key adds its tags to the
same set.

| Filter | Matches resources that |
| --- | --- |
| `tags` | have every listed tag. |
| `tags-any` | have at least one listed tag. |
| `not-tags` | do not have every listed tag. |
| `not-tags-any` | have none of the listed tags. |

For example, `?tags-any=red,blue` matches the same resources as
`?tags-any=red&tags-any=blue`. Different filters must all match, and they
combine with the other filters each list supports. An empty value names
the empty tag, so `?tags=` selects resources tagged with `""`. In
Gophercloud v2.15.0, set `Tags`, `TagsAny`, `NotTags`, or `NotTagsAny` in
the resource's `ListOpts`. Lists still reject unsupported keys, such as
`tag`, with `400`. See the [Neutron tag filters] and [Neutron filter
parsing].

## Omissions

- `POST /network/v2.0/{resources}/{id}/tags`, which adds tags without
  removing others, is not routed.
- The `tag-creation` extension is not implemented. Create requests that
  include `tags` are rejected as unknown fields.
- SandStack does not model `revision_number` or `updated_at`, so a tag
  change does not report a revision bump.
- Neutron also tags subnet pools, QoS policies, trunks, and network
  segment ranges. SandStack does not model those resources.
- Neutron's handling of a malformed JSON body on the replacement request
  could not be determined from source. SandStack returns
  `Invalid tags body` for it.

## Verification

Run `go test ./...`. The application tests cover replacement, addition,
removal, access rules, the tag limit, and the four filters. The
Gophercloud tests exercise every `attributestags` operation on networks
and ports, the list filters on networks and ports, and tag replacement
and filtering on the other four resources. The HTTP tests check the
status codes, bodies, and validation messages for all six collections,
along with the policy and unknown-resource errors and the filter parsing.
The filter tests follow the [Neutron tempest tag filter cases].

[Neutron tag controller]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/extensions/tagging.py#L206-L365
[Neutron tag plugin]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/services/tag/tag_plugin.py#L46-L145
[Gophercloud attributestags]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/networking/v2/extensions/attributestags/requests.go
[Neutron tag limits]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/extensions/tagging.py#L50-L125
[neutron-lib string validators]: https://github.com/openstack/neutron-lib/blob/5.0.0/neutron_lib/api/validators/__init__.py#L165-L346
[Neutron network tag policy]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/conf/policies/network.py#L265-L281
[Neutron subnet tag policy]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/conf/policies/subnet.py#L164-L172
[oslo.policy error]: https://github.com/openstack/oslo.policy/blob/6.0.0/oslo_policy/policy.py#L298-L308
[Neutron tag resource lookup]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/extensions/tagging.py#L151-L214
[Neutron exception translation]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/api/api_common.py#L453-L521
[Neutron tag filters]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/objects/tag.py#L30-L103
[Neutron filter parsing]: https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/api/api_common.py#L77-L118
[Neutron tempest tag filter cases]: https://github.com/openstack/neutron-tempest-plugin/blob/fb16056011dd71d4d0e768e9f61fc7aa2f4317c0/neutron_tempest_plugin/api/admin/test_tag.py#L256-L295
