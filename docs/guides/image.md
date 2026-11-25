# Image sandbox

Use the Image sandbox to test an application that follows a Nova server's
image reference, reads its metadata, and downloads the image data.
Authenticate with the [Identity fixture](identity.md), read the
[Compute fixture](compute.md), and take its `image.id`. Use Gophercloud's
`NewImageV2` to discover the public `image` endpoint in `RegionOne` and
`images.Get` to read that ID. The image is private to the fixture project.

The unversioned `/image/` URL returns the v2 discovery document. Use
`GET /image/v2/images/{image_id}` with `X-Auth-Token` for metadata.

## Download the fixture image

Use Gophercloud's `imagedata.Download(ctx, client, imageID).Extract()` to
open the image stream, and close it after reading. The request is
`GET /image/v2/images/{image_id}/file` with the project's `X-Auth-Token`.

The `200` response contains a 1,792-byte blank QCOW2 image with a virtual
size of 1 MiB. It sends `Content-Type: application/octet-stream`,
`Content-Length: 1792`, and a hexadecimal `Content-MD5` checksum. The
length and checksum match the image metadata. See the
[Glance download reference] and [Glance download serializer].

The fixture supports full downloads through the baseline Glance v2.0 API.
For an active image, range headers and query parameters such as `prefer`
receive `400`. Upload and schema documents are outside the fixture's scope.

## Deactivate and reactivate the fixture image

The image starts with status `active`. Set `image_id` to the Nova image
reference and send the SandStack admin token with the control request.

```sh
image_url="http://127.0.0.1:8080/_sandstack/image/images/${image_id}/deactivated"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$image_url"
```

The control returns `204`. Image metadata remains readable and reports
`deactivated`, while the Nova server still references the same image.
Downloading the file with the fixture project's token now returns `403`.
Send `DELETE` to the control URL to reactivate the image and download the
original bytes again. [Reset the sandbox](reset.md) also restores the
original image and requires a fresh token. See the [Glance download policy].

## Verification

Run `go test ./...` to follow the Nova image reference through real
Gophercloud clients, download the file, and observe deactivation and
recovery. The HTTP tests check the response headers, file metadata, and
download denial while deactivated. See the [Glance discovery reference],
[Glance image reference], and [Glance download reference].

[Glance discovery reference]: https://docs.openstack.org/api-ref/image/versions/index.html
[Glance image reference]: https://docs.openstack.org/api-ref/image/v2/#show-image
[Glance download reference]: https://docs.openstack.org/api-ref/image/v2/#download-binary-image-data
[Glance download policy]: https://github.com/openstack/glance/blob/master/glance/api/v2/image_data.py#L442-L459
[Glance download serializer]: https://github.com/openstack/glance/blob/master/glance/api/v2/image_data.py#L608-L651
