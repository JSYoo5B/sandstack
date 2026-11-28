# Image members in the Glance sandbox

[Image sandbox](image.md)

Share a `shared` image owned by one fixture project with another project.
Authenticate the owner with the [Identity fixture](identity.md) and the
member with an [additional fixture](identity.md#additional-fixtures).
The owner adds and removes members. Only the member project accepts or
rejects the share. Members apply only to images with `shared` visibility,
which is the default for [created images](image.md#create-a-queued-image).
See [Image visibility](image.md#image-visibility-across-projects), the
[Glance member controller], the [Glance member policies], and the
[Gophercloud member operations].

The behavior follows `openstack/glance` commit
`3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef`, the latest commit when
cross-project sharing was implemented, with the default policies and
`enforce_new_defaults` enabled.

## Add an image member

Use `members.Create(ctx, client, imageID, projectID)` in Gophercloud
v2.15.0 with the owner's token. It sends `POST /images/{image_id}/members`
with `{"member": "<project_id>"}`. The response is `200` with a `pending`
membership. The body contains `image_id`, `member_id`, `status`,
`created_at`, `updated_at`, and `schema`. Timestamps are UTC with second
precision, such as `2026-11-28T01:02:03Z`. The schema link is
`/image/v2/schemas/member`. See the [Glance member serializer].

SandStack does not check that the member project exists. Adding the same
project again returns `409`. An image accepts up to 128 members, and the
next addition returns `413`. A missing body, a missing or empty `member`,
or a non-string `member` returns `400`. See the [Glance member
deserializer] and the [Glance member quota].

An image the project cannot see returns `404`. An image that is not
`shared` returns `403`, including the `private` startup image. A member
project that adds another member receives `403`. The owner may add its
own project as a member.

## Read the shared image as a member

After the owner adds the member, the member's token reads the image with
`images.Get` and downloads its data, whatever the membership status. The
default `images.List` includes the image only after the member accepts it.
Set `images.ListOpts.MemberStatus` to `pending`, `rejected`, or `all` to
list shares in other states. See [Filter by visibility and
membership](image.md#filter-by-visibility-and-membership) and the [Glance
image visibility check].

## List and read members

Use `members.List` with the SDK pager and `members.ExtractMembers`. It
sends `GET /images/{image_id}/members` and receives `200` with `members`
and `schema: "/image/v2/schemas/members"`. The owner sees every member,
and a member project sees only its own membership. The list has one page
and is ordered by creation time and then member ID.

Use `members.Get` to read one membership through
`GET /images/{image_id}/members/{member_id}`. An unknown member, or
another project's membership read by a member, returns `404`. Reading a
member of an image that is not `shared` returns `404`, and listing its
members returns `403`. See the [Glance member lookup].

## Change the member status

Use `members.Update` with `members.UpdateOpts{Status: status}` and the
member's token. It sends `PUT /images/{image_id}/members/{member_id}` with
`{"status": "..."}`. The allowed values are `pending`, `accepted`, and
`rejected`. The member receives `400` for any other value and `200` with
the updated membership otherwise. `updated_at` changes only when the
status changes. A missing `status` returns `400`.

The owner receives `403` when it updates a membership, whatever status it
sends, including a membership for its own project. A member project can
reach only its own membership, so another member ID returns `404`. See the
[Glance member status] and the [Glance member lookup].

## Remove a member

Use `members.Delete` with the owner's token. It sends
`DELETE /images/{image_id}/members/{member_id}` and receives `204` with
an empty body. A later read or removal of the same member returns `404`.
The former member's image reads return `404`, and the image leaves its
lists. Adding the project again creates a new `pending` membership. A
member project that removes its own membership receives `403`.

Deleting the image removes its members. Changing the visibility away from
`shared` keeps the members but hides the image from them until the owner
sets `shared` again. A [sandbox reset](reset.md) removes all members.

## Discover member schemas

Follow the `schema` link from a member or member list with the same token.
`GET /image/v2/schemas/member` returns the member's `name` and
`properties`. `GET /image/v2/schemas/members` returns the `members` array
with the member schema as its items and a `describedby` link. See the
[Glance member schemas].

## Omissions

SandStack has no administrator or service token for the Image API, so the
administrator paths of the member policies are not available. Projects
cannot create public images, so no public image exists to share.

## Verification

Run `go test ./internal/app/image ./internal/api/server -count=1`. The
application tests cover duplicate and missing members, owner and member
permissions, the shared visibility requirement, self-membership, status
validation, the member limit, removal, and reset. The Gophercloud test
shares an image from the primary project with an additional fixture, which
reads it, accepts it, finds it in its list, and loses access when the owner
removes the membership. Another Gophercloud test creates a volume in the
additional project from the shared image. Raw HTTP tests check the
response fields, timestamp format, error statuses, cross-project `403` and
`404` cases, list filters, reset, and both member schemas.

[Glance member controller]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/image_members.py#L75-L344
[Glance member policies]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/policies/image.py#L223-L292
[Glance member lookup]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/sqlalchemy/api.py#L1427-L1448
[Glance image visibility check]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/utils.py#L44-L74
[Glance member deserializer]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/image_members.py#L347-L388
[Glance member serializer]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/image_members.py#L391-L435
[Glance member schemas]: https://github.com/openstack/glance/blob/12.0.0/glance/api/v2/image_members.py#L325-L372
[Glance member status]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/domain/__init__.py#L319-L339
[Glance member quota]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/quota/__init__.py#L184-L210
[Gophercloud member operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/members/requests.go#L28-L87
