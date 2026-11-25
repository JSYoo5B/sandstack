# Image sandbox

Use the Image sandbox to test an application that follows a Nova server's
image reference, reads its metadata, and downloads the image data.
Authenticate with the [Identity fixture](identity.md), read the
[Compute fixture](compute.md), and take its `image.id`. Use Gophercloud's
`NewImageV2` to discover the public `image` endpoint in `RegionOne` and
`images.Get` to read that ID. The image is private to the fixture project.

The unversioned `/image/` URL returns the v2 discovery document. Use
`GET /image/v2/images/{image_id}` with `X-Auth-Token` for metadata.

## Create a private queued image

Use `images.Create` in Gophercloud v2.15.0 with a nonempty `Name`.
It sends `POST /images` with an `application/json` body. SandStack's
v2.0 fixture defaults to `private`. You can also set `Visibility` to
`private` explicitly. The optional formats are `bare` and `qcow2`.
The response is `201` with a `queued` image owned by the authenticated
project. Its `Location` points to the image metadata. Omitted formats,
size, virtual size, and checksum are `null`. See the [Glance create
reference], [Glance v2.0 image defaults], and [Gophercloud image create
options].

Read the new image through `images.Get` and find it in `images.List`.
A file request for a queued image returns `204` with an empty body.
Creating an image leaves the startup fixture and its download controls
intact. A [sandbox reset](reset.md) removes queued images.
The fixture's create operation accepts the name, private visibility, and
the optional formats. A public request returns `403`; unsupported
fields and invalid values return `400`.

### Upload a queued image file

Create a queued image with `ContainerFormat: "bare"` and
`DiskFormat: "qcow2"`, then pass its ID and a reader to
`imagedata.Upload`. It sends `PUT /images/{image_id}/file` with
`Content-Type: application/octet-stream`. The response has an empty
`204` body after the image becomes `active`. Read its size and MD5
checksum through `images.Get` and retrieve the original bytes with
`imagedata.Download`. See the [Glance upload reference], [Glance upload
tests], and [Gophercloud image upload].

The server accepts up to 16 MiB per upload, including chunked bodies.
A missing or wrong media type returns `415`, missing formats return
`400`, and an upload to an active image returns `409`. A payload above
the limit returns `413`. A rejected upload leaves the image and existing
bytes intact. An uploaded empty file becomes active with size zero and the
MD5 of an empty payload. Its full download returns `200` with zero bytes.
See the [Glance filesystem store].

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

## List and filter images

Use `GET /image/v2/images` with the fixture project's token to list its
images. In Gophercloud v2.15.0, call `images.List` with `images.ListOpts`.
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
including repeated tag values, and a `schema` link. A query with no matches
returns `images: []`. SandStack accepts one value each for `name` and
`status`. Repeating either key, using an unsupported query key, or using
a name or status comparison operator other than `eq:` returns `400`. See the [Glance
image list serializer].

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

Use Gophercloud's `imagedata.Download(ctx, client, imageID).Extract()` to
open the image stream, and close it after reading. The request is
`GET /image/v2/images/{image_id}/file` with the project's `X-Auth-Token`.

The `200` response contains a 1,792-byte blank QCOW2 image with a virtual
size of 1 MiB. It sends `Content-Type: application/octet-stream`,
`Content-Length: 1792`, and a hexadecimal `Content-MD5` checksum. The
length and checksum match the image metadata. See the
[Glance download reference] and [Glance download serializer].

## Download one byte range

Use the same file URL with a single `Range` header. A closed range, an
open-ended range, and a suffix range all return `206` and the selected
bytes. For the seeded image the following requests each return four bytes.

| Request `Range` | Response `Content-Range` |
| --- | --- |
| `bytes=0-3` | `bytes 0-3/1792` |
| `bytes=1788-` | `bytes 1788-1791/1792` |
| `bytes=-4` | `bytes 1788-1791/1792` |

`Content-Length` gives the number of bytes in the response body.
`Content-MD5` still matches the checksum of the complete image. An end
offset past the file is clipped to the final byte. A suffix at least as
large as the file returns all bytes with `206`.

Multiple ranges or repeated `Range` headers receive `400`. A malformed
range or a start offset at or past the end receives `416` with
`Content-Range: bytes */1792` for this fixture. See the [Glance range parser],
[Glance download serializer], and [HTTP range semantics].

Use a low-level Gophercloud request to send the range and accept `206`.
`imagedata.Download` remains the full-download helper.

```go
response, err := client.Get(
    ctx, client.ServiceURL("images", imageID, "file"), nil,
    &gophercloud.RequestOpts{
        MoreHeaders:     map[string]string{"Range": "bytes=0-3"},
        OkCodes:         []int{http.StatusPartialContent},
        KeepResponseBody: true,
    },
)
```

On success, read and close `response.Body`. The fixture supports full and
single-range downloads through the baseline Glance v2.0 API. On an active
image, the legacy request `Content-Range` header and query parameters such
as `prefer` receive `400`. Upload is outside this scope.

## Deactivate and reactivate the fixture image

The image starts with status `active`. Set `image_id` to the Nova image
reference and send the SandStack admin token with the control request.

```sh
image_url="http://127.0.0.1:8080/_sandstack/image/images/${image_id}/deactivated"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$image_url"
```

The control returns `204`. Image metadata remains readable and reports
`deactivated`, while the Nova server still references the same image.
Full and ranged downloads with the fixture project's token now return `403`.
Send `DELETE` to the control URL to reactivate the image and download the
original bytes again. [Reset the sandbox](reset.md) also restores the
original image and requires a fresh token. See the [Glance download policy].

## Fail the next image download

Send `PUT` to
`/_sandstack/image/images/{image_id}/unavailable-download` with
`X-SandStack-Admin-Token`. The control returns `204`. The next valid
download of the active image returns `503`. This also applies to a
valid byte-range request. Glance returns `503` when its image store is
temporarily unavailable. SandStack sends a text error without image data,
an image checksum, or a content range. See the [Glance store error
handling].

Retry the same download to read the original bytes and checksum. Image
metadata and timestamps remain stable. Metadata requests, unauthorized
requests, downloads of a missing or deactivated image, and invalid
download requests leave the failure pending. Repeating `PUT` schedules
one failure. Send `DELETE` to cancel it. A [sandbox reset](reset.md)
also clears it.

## Verification

Run `go test ./...` to follow the Nova image reference through real
Gophercloud clients, download the file, and observe deactivation and
recovery. The HTTP tests check the response headers, file metadata, and
download denial while deactivated. Range tests check the exact bytes and
headers of a partial response and the rejection statuses. See the
[Glance discovery reference], [Glance image reference], and
[Glance download reference].
The list tests exercise exact filters and the live activation state
through Gophercloud and check the HTTP response shape and query errors.
The rename tests follow the new name through detail and filtered list
reads and check media-type negotiation over HTTP.
The tag tests check replacement, clearing, validation, and matching all
requested tags in the image list.
Schema tests resolve the links from detail and list responses and check
the fixture response types against the returned schemas.
The transient download test checks `503`, a byte-identical SDK retry,
and a ranged retry over HTTP.
The creation tests check the location and queued metadata over HTTP and
read the new record through SDK detail and paginated list requests.
The upload tests check size, checksum, and complete and ranged reads.
ID filter tests use several images to check exact and multi-ID selection
and preservation of the filter across SDK pages.

[Glance discovery reference]: https://docs.openstack.org/api-ref/image/versions/index.html
[Glance image reference]: https://docs.openstack.org/api-ref/image/v2/#show-image
[Glance create reference]: https://docs.openstack.org/api-ref/image/v2/#create-image
[Glance delete reference]: https://docs.openstack.org/api-ref/image/v2/#delete-image
[Glance upload reference]: https://docs.openstack.org/api-ref/image/v2/#upload-binary-image-data
[Glance upload tests]: https://github.com/openstack/glance/blob/12.0.0/glance/tests/functional/v2/test_images.py#L447-L497
[Glance filesystem store]: https://github.com/openstack/glance_store/blob/0.13.0/glance_store/_drivers/filesystem.py#L592-L638
[Glance v2.0 image defaults]: https://github.com/openstack/glance/blob/12.0.0/glance/domain/__init__.py#L71-L93
[Gophercloud image create options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L145-L218
[Gophercloud image delete]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L221-L226
[Gophercloud image upload]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/imagedata/requests.go#L10-L18
[Glance image schema reference]: https://docs.openstack.org/api-ref/image/v2/#show-image-schema
[Glance collection schema reference]: https://docs.openstack.org/api-ref/image/v2/#show-images-schema
[Glance schema builders]: https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L1963-L2008
[Gophercloud image result]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/results.go#L81-L90
[Glance list reference]: https://docs.openstack.org/api-ref/image/v2/#list-images
[Glance image list serializer]: https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L1697-L1713
[Glance image pagination]: https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L497-L534
[Glance marker lookup]: https://github.com/openstack/glance/blob/master/glance/db/sqlalchemy/api.py#L664-L679
[Glance image list filters]: https://github.com/openstack/glance/blob/master/glance/db/sqlalchemy/api.py#L508-L524
[Glance tag filters]: https://github.com/openstack/glance/blob/master/glance/db/sqlalchemy/api.py#L470-L476
[Glance ID filters]: https://github.com/openstack/glance/blob/12.0.0/glance/db/sqlalchemy/api.py#L515-L530
[Glance filter operands]: https://github.com/openstack/glance/blob/12.0.0/glance/common/utils.py#L594-L659
[Gophercloud image list options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L25-L78
[Gophercloud image next links]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/urls.go#L42-L64
[Glance update reference]: https://docs.openstack.org/api-ref/image/v2/#update-image
[Glance patch media-type handling]: https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L1380-L1395
[Glance update serializer]: https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L1693-L1696
[Gophercloud image update]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L236-L248
[Gophercloud image tag replacement]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L335-L347
[Glance tag schema]: https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L1821-L1828
[Glance image tag quota]: https://docs.openstack.org/glance/latest/configuration/glance_api.html#DEFAULT.image_tag_quota
[Glance download reference]: https://docs.openstack.org/api-ref/image/v2/#download-binary-image-data
[Glance download policy]: https://github.com/openstack/glance/blob/master/glance/api/v2/image_data.py#L442-L459
[Glance range parser]: https://github.com/openstack/glance/blob/master/glance/common/wsgi.py#L292-L314
[Glance download serializer]: https://github.com/openstack/glance/blob/master/glance/api/v2/image_data.py#L569-L651
[Glance store error handling]: https://github.com/openstack/glance/blob/stable/2025.1/glance/api/v2/image_data.py#L491-L525
[HTTP range semantics]: https://www.rfc-editor.org/rfc/rfc9110.html#name-content-range
