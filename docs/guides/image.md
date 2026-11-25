# Image sandbox

Use the Image sandbox to test an application that follows a Nova server's
image reference, reads its metadata, and downloads the image data.
Authenticate with the [Identity fixture](identity.md), read the
[Compute fixture](compute.md), and take its `image.id`. Use Gophercloud's
`NewImageV2` to discover the public `image` endpoint in `RegionOne` and
`images.Get` to read that ID. The image is private to the fixture project.

The unversioned `/image/` URL returns the v2 discovery document. Use
`GET /image/v2/images/{image_id}` with `X-Auth-Token` for metadata.

## List and filter images

Use `GET /image/v2/images` with the fixture project's token to list its
image. In Gophercloud v2.15.0, call `images.List` with `images.ListOpts`.
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

The response includes `images`, a `first` link that preserves the filters,
including repeated tag values, and a `schema` link. A query with no matches
returns `images: []`. The single-image fixture returns one page. SandStack
accepts one value each for `name` and `status`. Repeating either of those
keys, using other query keys, or using a `name` or `status` operator other
than `eq:` returns `400`. See the [Glance image list serializer].

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
as `prefer` receive `400`. Upload and schema documents are outside this
scope.

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

[Glance discovery reference]: https://docs.openstack.org/api-ref/image/versions/index.html
[Glance image reference]: https://docs.openstack.org/api-ref/image/v2/#show-image
[Glance list reference]: https://docs.openstack.org/api-ref/image/v2/#list-images
[Glance image list serializer]: https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L1697-L1713
[Glance image list filters]: https://github.com/openstack/glance/blob/master/glance/db/sqlalchemy/api.py#L508-L524
[Glance tag filters]: https://github.com/openstack/glance/blob/master/glance/db/sqlalchemy/api.py#L470-L476
[Gophercloud image list options]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/images/requests.go#L25-L78
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
[HTTP range semantics]: https://www.rfc-editor.org/rfc/rfc9110.html#name-content-range
