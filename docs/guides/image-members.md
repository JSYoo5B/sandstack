# Image members in the Glance sandbox

[Image sandbox](image.md)

Use the project token from the [Identity fixture](identity.md) to share a
private image owned by the fixture project with another project. SandStack
follows the Image API v2.0 sharing model it advertises in discovery: a
non-public image accepts members, and the owner adds or removes them. The
member project accepts or rejects the share. See the [Glance member
authorization] and [Gophercloud member operations].

## Add an image member

Use `members.Create(ctx, client, imageID, projectID)` in Gophercloud
v2.15.0. It sends `POST /images/{image_id}/members` with
`{"member": "<project_id>"}`. The response is `200` with a `pending`
membership. The body contains `image_id`, `member_id`, `status`,
`created_at`, `updated_at`, and `schema`. Timestamps are UTC with second
precision, such as `2026-11-28T01:02:03Z`. The schema link is
`/image/v2/schemas/member`. See the [Glance member controller] and
[Glance member serializer].

SandStack does not check that the member project exists. Adding the same
project again returns `409`. An image accepts up to 128 members, and the
next addition returns `413`. A missing body, a missing or empty `member`,
or a non-string `member` returns `400`. An image the project does not own
returns `404`. See the [Glance member deserializer] and the [Glance member
quota].

## List and read members

Use `members.List` with the SDK pager and `members.ExtractMembers`. It
sends `GET /images/{image_id}/members` and receives `200` with `members`
and `schema: "/image/v2/schemas/members"`. The list has one page and is
ordered by creation time and then member ID. Use `members.Get` to read one
membership through `GET /images/{image_id}/members/{member_id}`. An
unknown member returns `404`.

## Change the member status

Use `members.Update` with `members.UpdateOpts{Status: status}`. It sends
`PUT /images/{image_id}/members/{member_id}` with `{"status": "..."}`.
Only the member project can change its own status. The owner receives
`403` when it updates another project's membership, whatever status it
sends. The allowed values are `pending`, `accepted`, and `rejected`. The
member receives `400` for any other value and `200` with the updated
membership otherwise. `updated_at` changes only when the status changes.
A missing `status` returns `400`. See the [Glance member status] and the
[Glance member lifecycle test].

A token from an [additional fixture](identity.md#additional-fixtures)
project cannot yet read an image shared with it or change its membership,
and receives `404`. Image API v2.0 lets an owner add its own project as a
member, and then that token can change the membership status. Use this
self-membership to exercise the successful status update path.

## Remove a member

Use `members.Delete`. It sends `DELETE /images/{image_id}/members/{member_id}`
and receives `204` with an empty body. A later read or removal of the same
member returns `404`. Adding the project again creates a new `pending`
membership.

Deleting the image removes its members. A [sandbox reset](reset.md)
removes all members, including those of the startup image.

## Discover member schemas

Follow the `schema` link from a member or member list with the same token.
`GET /image/v2/schemas/member` returns the member's `name` and
`properties`. `GET /image/v2/schemas/members` returns the `members` array
with the member schema as its items and a `describedby` link. See the
[Glance member schemas].

## Omissions

The fixture has no second project, so SandStack does not expose a shared
image to a member project. Image list and detail requests show only images
the project owns, and member-side reads and the `member_status` and
`visibility` list filters are not available. Public images do not exist
in the sandbox, so the `403` returned for public image members cannot occur.

## Verification

The application tests cover duplicate and missing members, owner and
member permissions, status validation, the member limit, removal, and
reset. The Gophercloud test adds, reads, lists, updates, and removes
members on the fixture image. Raw HTTP tests check the response fields,
timestamp format, error statuses, reset, and both member schemas.

[Glance member authorization]: https://github.com/openstack/glance/blob/12.0.0/glance/api/authorization.py#L115-L227
[Glance member controller]: https://github.com/openstack/glance/blob/12.0.0/glance/api/v2/image_members.py#L91-L231
[Glance member deserializer]: https://github.com/openstack/glance/blob/12.0.0/glance/api/v2/image_members.py#L234-L275
[Glance member serializer]: https://github.com/openstack/glance/blob/12.0.0/glance/api/v2/image_members.py#L278-L322
[Glance member schemas]: https://github.com/openstack/glance/blob/12.0.0/glance/api/v2/image_members.py#L325-L372
[Glance member status]: https://github.com/openstack/glance/blob/12.0.0/glance/domain/__init__.py#L305-L335
[Glance member quota]: https://github.com/openstack/glance/blob/12.0.0/glance/quota/__init__.py#L175-L191
[Glance member lifecycle test]: https://github.com/openstack/glance/blob/12.0.0/glance/tests/functional/v2/test_images.py#L3245-L3527
[Gophercloud member operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/members/requests.go#L28-L87
