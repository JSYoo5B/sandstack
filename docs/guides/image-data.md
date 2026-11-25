# Image data in the Glance sandbox

[Image sandbox](image.md)

Use the project token and image ID from the Image sandbox. To test
upload, first [create a queued image](image.md#create-a-private-queued-image).

## Upload a queued image file

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

Run `go test ./...` to exercise uploads and downloads through Gophercloud.
The HTTP tests check the response headers, file metadata, and
download denial while deactivated. Range tests check the exact bytes and
headers of a partial response and the rejection statuses.
The transient download test checks `503`, a byte-identical SDK retry,
and a ranged retry over HTTP.
The upload tests check size, checksum, and complete and ranged reads.

[Glance upload reference]: https://docs.openstack.org/api-ref/image/v2/#upload-binary-image-data
[Glance upload tests]: https://github.com/openstack/glance/blob/12.0.0/glance/tests/functional/v2/test_images.py#L447-L497
[Glance filesystem store]: https://github.com/openstack/glance_store/blob/0.13.0/glance_store/_drivers/filesystem.py#L592-L638
[Gophercloud image upload]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/image/v2/imagedata/requests.go#L10-L18
[Glance download reference]: https://docs.openstack.org/api-ref/image/v2/#download-binary-image-data
[Glance download policy]: https://github.com/openstack/glance/blob/master/glance/api/v2/image_data.py#L442-L459
[Glance range parser]: https://github.com/openstack/glance/blob/master/glance/common/wsgi.py#L292-L314
[Glance download serializer]: https://github.com/openstack/glance/blob/master/glance/api/v2/image_data.py#L569-L651
[Glance store error handling]: https://github.com/openstack/glance/blob/stable/2025.1/glance/api/v2/image_data.py#L491-L525
[HTTP range semantics]: https://www.rfc-editor.org/rfc/rfc9110.html#name-content-range
