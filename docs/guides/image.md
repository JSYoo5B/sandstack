# Image sandbox

Use the Image sandbox to test an application that reads a Nova server's
image metadata even when the referenced image becomes unavailable for use.
Authenticate with the [Identity fixture](identity.md), read the
[Compute fixture](compute.md), and take its `image.id`. Use Gophercloud's
`NewImageV2` to discover the public `image` endpoint in `RegionOne` and
`images.Get` to read that ID. The image is private to the fixture project.

The unversioned `/image/` URL returns the v2 discovery document. Use
`GET /image/v2/images/{image_id}` with `X-Auth-Token` for metadata. Image
files and schema documents are outside this scenario.

## Deactivate and reactivate the fixture image

The image starts with status `active`. Set `image_id` to the Nova image
reference and send the SandStack admin token with the control request.

```sh
image_url="http://127.0.0.1:8080/_sandstack/image/images/${image_id}/deactivated"
curl --fail -X PUT -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$image_url"
```

The control returns `204`. Image metadata remains readable and reports
`deactivated`, while the Nova server still references the same image.
Send `DELETE` to the control URL to reactivate the image, or
[reset the sandbox](reset.md) to restore the original fixture.

## Verification

Run `go test ./...` to follow the Nova image reference through real
Gophercloud clients and observe `active` and `deactivated`. The HTTP tests
use the [Glance discovery reference] and [Glance image reference].

[Glance discovery reference]: https://docs.openstack.org/api-ref/image/versions/index.html
[Glance image reference]: https://docs.openstack.org/api-ref/image/v2/#show-image
