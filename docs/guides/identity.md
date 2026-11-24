# Identity sandbox

Use the Identity sandbox to test how a client reacts when a previously valid
token stops working. The normal path obtains a project-scoped token, uses it
to read the service catalog, and then repeats the request after SandStack
invalidates the issued tokens.

## Configuration

Start the server with the fixture variables shown in the
[README](../../README.md#run-the-identity-sandbox). The server provides one
user and one project in the `Default` domain (`default`), with one member
role. Use disposable credentials and a separate admin token. Tokens expire
after one hour and live in memory for that server instance.

`SANDSTACK_LISTEN_ADDR` overrides the loopback listen address. Set
`SANDSTACK_PUBLIC_URL` to an externally reachable HTTP or HTTPS origin when
using a proxy or when the client-facing authority differs from the request
host. The catalog otherwise uses the request's host and HTTP scheme or TLS
state. `GET /_sandstack/health` and `GET /_sandstack/ready` return `204` when
the server is running.

## Supported operations

| Request | Behavior |
| --- | --- |
| `POST /identity/v3/auth/tokens` | Project-scoped password authentication. A user or project can be selected by ID or by name with a domain. Success returns `201` and `X-Subject-Token`. Wrong credentials return `401`. The `nocatalog` parameter omits the catalog from the token document. |
| `GET /identity/v3/auth/catalog` | Returns the catalog for a valid `X-Auth-Token`. It contains the public Identity endpoint in `RegionOne`. Missing, expired, and invalidated tokens receive `401`. |
| `HEAD /identity/v3/auth/catalog` | Returns the catalog request status and headers with an empty body. |
| `POST /_sandstack/identity/invalidate-tokens` | Requires `X-SandStack-Admin-Token`. Invalidates all tokens issued by that server and returns `204`. Fresh authentication issues a usable token. |

Use a versioned Identity URL. This scenario is limited to password
authentication, a single project scope, and catalog reads.

After a successful catalog read, invalidate the sandbox's tokens to exercise
the client's recovery path.

```sh
curl --fail -X POST \
  -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" \
  http://127.0.0.1:8080/_sandstack/identity/invalidate-tokens
```

## Verification

Run `go test ./...` to exercise the assembled server with raw HTTP and
Gophercloud v2.15.0. The client scenario authenticates, selects the catalog
endpoint, observes the failure after invalidation, and authenticates again.
The wire checks use the [Identity v3 API reference] and the
[Keystone catalog handler] as sources. The catalog flow follows the
[Keystone default policy] for project-scoped tokens.

[Identity v3 API reference]: https://docs.openstack.org/api-ref/identity/v3/
[Keystone catalog handler]: https://docs.openstack.org/keystone/latest/_modules/keystone/api/auth.html
[Keystone default policy]: https://github.com/openstack/keystone/blob/master/keystone/common/policies/auth.py
