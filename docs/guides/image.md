# Image sandbox

Use the Image sandbox to test an application that follows a Nova server's
image reference, reads its metadata, and downloads the image data.
Authenticate with the [Identity fixture](identity.md), read the
[Compute fixture](compute.md), and take its `image.id`. Use Gophercloud's
`NewImageV2` to discover the public `image` endpoint in `RegionOne` and
`images.Get` to read that ID. The image is private to the fixture project.

The unversioned `/image/` URL returns the v2 discovery document. Use
`GET /image/v2/images/{image_id}` with `X-Auth-Token` for metadata.

## Create a queued image

Use `images.Create` in Gophercloud v2.15.0 with a nonempty `Name` and the
token of any fixture project. It sends `POST /images` with an
`application/json` body. The image defaults to `shared` visibility, as in
Glance. Set `Visibility` to `private`, `shared`, or `community` to choose
another. The optional formats are `bare` and `qcow2`. The response is
`201` with a `queued` image owned by the authenticated project. Its
`Location` points to the image metadata. Omitted formats, size, virtual
size, and checksum are `null`. See the [Glance create reference],
[Glance image defaults], and [Gophercloud image create options].

Read the new image through `images.Get` and find it in `images.List`.
Images that Nova creates from [server snapshots](compute-snapshots.md)
also carry additional properties, which appear as top-level string fields
and in Gophercloud's `Image.Properties`. The create operation does not
accept additional properties.
Cinder creates `shared` images when it [uploads a
volume](volume-upload-image.md).
A file request for a queued image returns `204` with an empty body.
Creating an image leaves the startup fixture and its download controls
intact. A [sandbox reset](reset.md) removes queued images.
The create operation accepts the name, the visibility, the optional
formats, capacity minimums, and the protection flag. A `public` request
returns `403`, because Glance reserves public images for administrators.
Unsupported fields and invalid values return `400`. See the [Glance
visibility policies].

Set `images.CreateOpts.MinDisk` in GiB and `MinRAM` in MiB to record the
image's minimum capacity requirements. Both default to zero and are
returned by create, detail, and list. A negative, null, or non-integer
value returns `400`. See the [Glance capacity requirements].

### Upload a queued image file

See [Upload a queued image file](image-data.md#upload-a-queued-image-file).

### Delete an image

Use `images.Delete` on an image owned by the fixture project. It sends
`DELETE /images/{image_id}` and receives `204` with an empty body. The
image disappears from the list, and subsequent detail, download, or
delete requests for its ID return `404`. A list request with that ID as
its marker returns `400`. See the [Glance delete reference] and
[Gophercloud image delete].

Deleting the startup image also removes its stored bytes from the
download path and clears its pending download failure. The Nova server
keeps the same image ID. A [sandbox reset](reset.md) restores the startup
image and its file and removes all client-created images.

### Protect an image from deletion

Set `images.CreateOpts.Protected` to a pointer to `true` when creating an
image, or use `images.ReplaceImageProtected{NewProtected: true}` in
`images.Update`. The update sends one JSON Patch `replace` for
`/protected` and returns `200` with the updated image. Detail and list
responses expose the same flag. The default is `false`. An explicit
protection value must be a JSON boolean. See the [Glance protection tests]
and [Gophercloud image protection].

A protected image returns `403` on deletion and remains readable with
its existing metadata and bytes. Set `NewProtected: false` to allow
deletion again. See the [Glance protected image error].

## Image visibility across projects

Each image records its owning project and a visibility. A project token
sees an image when it owns it, when the image is `public` or `community`,
or when the image is `shared` and the project is a
[member](image-members.md) in any status. Detail, download, and member
requests for an image the project cannot see return `404`, and a list
marker that names it returns `403`. The startup image stays `private` to the
primary [Identity fixture](identity.md) project, so an [additional
fixture](identity.md#additional-fixtures) project cannot read it. See the
[Glance image visibility check].

Only the owner changes an image. Update, delete, and upload requests from
another project return `403` when that project can see the image and
`404` otherwise. Any project downloads the data of an image it can see.
See the [Glance image policy enforcement] and the [Glance image policy
defaults].

Use `images.UpdateVisibility{Visibility: visibility}` as the only
operation in `images.UpdateOpts` to change an owned image between
`private`, `shared`, and `community`. It sends one JSON Patch `replace`
for `/visibility` and returns `200` with the updated image. A `public`
value returns `403`, and other values return `400`. Members of an image
that leaves `shared` lose access until it returns to `shared`. See the
[Glance visibility policies] and [Gophercloud image visibility update].

The create defaults, visibility, ownership, and list filter behavior
follow `openstack/glance` commit
`3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef`, the latest commit when
cross-project images were implemented, with the default policies and
`enforce_new_defaults` enabled.

## List and filter images

Use `GET /image/v2/images` with a project token to list the images that
project can see. By default the list contains the project's own images,
public images, and `shared` images whose membership the project accepted.
Another project's `community` images appear only with a visibility
filter, as described in [Filter by visibility and
membership](#filter-by-visibility-and-membership). In Gophercloud v2.15.0,
call `images.List` with `images.ListOpts`.
Set `Name`, `Status`, or both for exact matching. Both fields must match
when supplied together. Use a bare value or the `eq:` prefix. For example,
`?name=fixture-image&status=eq:active` selects the fixture while it is
active. The list reflects status changes immediately after deactivation or
reactivation. See the [Glance list reference], [Glance image list filters],
and [Gophercloud image list options].

Pass `Tags` in `images.ListOpts` to require tags. Gophercloud sends each
entry as a separate `tag` query value. For example, `?tag=ready&tag=approved`
requires both tags. SandStack trims surrounding whitespace on each query
value before matching. Tag filters combine with `name` and `status` when
present. See the [Glance list reference] and [Glance tag filters].

Set `images.ListOpts.ID` to an ID or `eq:<id>` for an exact match.
Use `in:<id>,<id>` to match any listed ID. Double quotes can enclose
an individual operand, and spaces outside quotes remain part of the
value. The ID filter combines with the name, status, and tag filters.
An unknown ID or empty operand matches no image. Unsupported operators,
malformed quotes, and repeated `id` parameters return `400`. See the
[Glance ID filters] and [Glance filter operands].

The response includes `images`, a `first` link that preserves the filters,
including repeated tag values, and a `schema` link. Glance builds its
links from one value per query key, so SandStack keeps more tag values
than Glance does. A query with no matches returns `images: []`. SandStack
accepts one value each for `name`, `status`, `visibility`, and
`member_status`. Repeating any of these keys, using an unsupported query
key, or using a name or status comparison operator other than `eq:`
returns `400`. See the [Glance image list serializer].

### Filter by visibility and membership

Set `images.ListOpts.Visibility` to `public`, `community`, `shared`, or
`private` to list only images with that visibility, or send
`visibility=all` to drop the default exclusion of other projects'
`community` images. `private` lists only the project's own private
images. Set `images.ListOpts.MemberStatus` to `pending`, `accepted`,
`rejected`, or `all` to choose which memberships include another
project's `shared` images. The default is `accepted`. The project's own
images are listed whatever their memberships. Other values of either
parameter return `400`. See the [Glance list query validation] and the
[Glance visibility list query].


### Page through the image list

Set `Limit` and `Marker` in `images.ListOpts` and use the SDK pager to
follow `next`. The default limit is 25 and the maximum is 1,000. A raw
`limit=0` returns an empty array. Pages list newer images first and use
descending ID order to break timestamp ties. A full page contains a
`next` link, so on a fresh sandbox `Limit: 1` returns the fixture and
links to an empty page. The
marker must identify an image visible to the project even when current
filters exclude it. An unknown or empty marker returns `400`. See the
[Glance image pagination] and [Glance marker lookup].

The `first` link omits the marker. Both links retain the other parameters,
including every tag value. SandStack emits `next` relative to the Image
catalog endpoint, as expected by Gophercloud v2.15.0. See the [Glance image
list serializer] and [Gophercloud image next links].

## Share an image with another project

See [Image members in the Glance sandbox](image-members.md).

## Discover image schemas

Follow the `schema` link in an image detail or list response using the
same project token. An image links to `GET /image/v2/schemas/image`. The
list links to `GET /image/v2/schemas/images`. Both return `200` with a
Glance-style JSON schema document. The item schema describes fixture
properties such as `id`, `status`, `tags`, `size`, and `checksum`. The
collection schema describes the `images` array and its item schema.
See the [Glance image schema reference], [Glance collection schema reference],
and [Glance schema builders].

With Gophercloud v2.15.0, resolve the returned path against the Image
endpoint's origin and fetch the resulting URL with `ServiceClient.Get`.
Decode the response into a JSON object. The SDK exposes the detail link
as `Image.Schema` and the list link is in the raw response envelope.
See the [Gophercloud image result].

## Rename the fixture image

Use `PATCH /image/v2/images/{image_id}` with the owning project's token.
The sandbox accepts `Content-Type: application/openstack-images-v2.1-json-patch`
and a patch array with one `replace` operation for `/name`. The `value`
must be a string of at most 255 characters. An unsupported media type
returns `415` with `Accept-Patch` advertising the supported type. See the
[Glance update reference] and [Glance patch media-type handling].

Gophercloud v2.15.0 sets the media type when you call `images.Update`.

```go
image, err := images.Update(ctx, client, imageID, images.UpdateOpts{
    images.ReplaceImageName{NewName: "renamed-fixture-image"},
}).Extract()
```

The response is `200` with the full image representation. Subsequent
`images.Get` and `images.List` show the new name. The image ID, activation
status, and file data stay the same. See the [Glance update serializer]
and [Gophercloud image update].

## Replace the fixture image tags

Use the same image PATCH request with one `replace` operation for `/tags`.
With Gophercloud v2.15.0, pass `images.ReplaceImageTags{NewTags: tags}`
as the only operation in `images.UpdateOpts`. Supply `[]string{}` to clear
all tags. A nil slice sends JSON `null` and returns `400`. See the
[Gophercloud image tag replacement].

Each tag is a string of at most 255 characters. SandStack limits the input
array to 128 entries before removing duplicates. Exceeding the limit
returns `413`. The returned tags are distinct and their order is
unspecified. See the [Glance tag schema] and [Glance image tag quota].

## Download the fixture image

See [Download the fixture image](image-data.md#download-the-fixture-image).

## Download one byte range

See [Download one byte range](image-data.md#download-one-byte-range).

## Deactivate and reactivate the fixture image

See [Deactivate and reactivate the fixture image](image-data.md#deactivate-and-reactivate-the-fixture-image).

## Fail the next image download

See [Fail the next image download](image-data.md#fail-the-next-image-download).

## Verification

Run `go test ./...` to resolve the Nova image reference and read its
metadata with Gophercloud. See the [Glance discovery reference] and
[Glance image reference].
The list tests exercise exact filters and the live activation state
through Gophercloud and check the HTTP response shape and query errors.
The rename tests follow the new name through detail and filtered list
reads and check media-type negotiation over HTTP.
The tag tests check replacement, clearing, validation, and matching all
requested tags in the image list.
Schema tests resolve the links from detail and list responses and check
the fixture response types against the returned schemas.
The creation tests check the location and queued metadata over HTTP and
read the new record through SDK detail and paginated list requests.
ID filter tests use several images to check exact and multi-ID selection
and preservation of the filter across SDK pages.
Visibility tests check reads, lists, filters, markers, and owner-only
changes for each visibility and membership status in the application,
and the cross-project `403` and `404` cases over HTTP.

[Glance discovery reference]: https://docs.openstack.org/api-ref/image/versions/index.html
[Glance image reference]: https://docs.openstack.org/api-ref/image/v2/#show-image
[Glance create reference]: https://docs.openstack.org/api-ref/image/v2/#create-image
[Glance delete reference]: https://docs.openstack.org/api-ref/image/v2/#delete-image
[Glance protection tests]: https://github.com/openstack/glance/blob/12.0.0/glance/tests/functional/v2/test_images.py#L553-L575
[Glance protected image error]: https://github.com/openstack/glance/blob/12.0.0/glance/common/exception.py#L123-L125
[Glance image defaults]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/domain/__init__.py#L71-L94
[Glance image visibility check]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/utils.py#L44-L74
[Glance image policy enforcement]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/policy.py#L139-L222
[Glance image policy defaults]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/policies/base.py#L36-L74
[Gophercloud image visibility update]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L279-L291
[Glance visibility policies]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/policies/image.py#L94-L116
[Glance capacity requirements]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/domain/__init__.py#L222-L246
[Gophercloud image create options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L145-L218
[Gophercloud image delete]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L221-L226
[Gophercloud image protection]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L377-L390
[Glance image schema reference]: https://docs.openstack.org/api-ref/image/v2/#show-image-schema
[Glance collection schema reference]: https://docs.openstack.org/api-ref/image/v2/#show-images-schema
[Glance schema builders]: https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L1963-L2008
[Gophercloud image result]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/results.go#L81-L90
[Glance list reference]: https://docs.openstack.org/api-ref/image/v2/#list-images
[Glance image list serializer]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/images.py#L1855-L1871
[Glance list query validation]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/images.py#L1582-L1599
[Glance visibility list query]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/sqlalchemy/api.py#L588-L625
[Glance image pagination]: https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L497-L534
[Glance marker lookup]: https://github.com/openstack/glance/blob/master/glance/db/sqlalchemy/api.py#L664-L679
[Glance image list filters]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/sqlalchemy/api.py#L556-L568
[Glance tag filters]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/sqlalchemy/api.py#L513-L519
[Glance ID filters]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/sqlalchemy/api.py#L556-L568
[Glance filter operands]: https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/common/utils.py#L749-L822
[Gophercloud image list options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L25-L78
[Gophercloud image next links]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/urls.go#L42-L64
[Glance update reference]: https://docs.openstack.org/api-ref/image/v2/#update-image
[Glance patch media-type handling]: https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L1380-L1395
[Glance update serializer]: https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L1693-L1696
[Gophercloud image update]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L236-L248
[Gophercloud image tag replacement]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L335-L347
[Glance tag schema]: https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L1821-L1828
[Glance image tag quota]: https://docs.openstack.org/glance/latest/configuration/glance_api.html#DEFAULT.image_tag_quota
