# Identity sandbox

Use the Identity sandbox to test how a client reacts when a previously valid
token stops working. The normal path obtains a project-scoped token, uses it
to read the service catalog, and then repeats the request after SandStack
invalidates the issued tokens.

## Configuration

Start the server with the fixture variables shown in the
[README](../../README.md#run-the-sandbox). The server provides one
user and one project in the `Default` domain (`default`), with one member
role. Use disposable credentials and a separate admin token. Tokens expire
after one hour and live in memory for that server instance.

## Additional fixtures

Scenarios that cross a project boundary can configure more fixtures with
numbered variables. Each fixture is one user with the `member` role on its
own project in the `Default` domain.

```sh
export SANDSTACK_FIXTURE_1_USERNAME=second-user
export SANDSTACK_FIXTURE_1_PASSWORD=second-password
export SANDSTACK_FIXTURE_1_PROJECT=second-project
```

Number fixtures from `1` without gaps, and set all three variables for each
one. User names and project names must differ from every other fixture,
including the primary one, and no password may equal the admin token. Other
`SANDSTACK_FIXTURE_` variables stop the server at startup. The user and
project IDs are 32 hexadecimal digits derived from the names, so they stay
the same across restarts. This configuration is interim, as described in
[ADR 0010](../decisions/0010-interim-fixture-projects.md).

A fixture user authenticates only with its own project scope. Scoping to
another fixture's project with a correct password returns `401` with the
message `The request you have made requires authentication.`, which is the
error Keystone returns when it cannot mint a project-scoped token for a user
without a role on the project. See the [Keystone project scope check], the
[Keystone Unauthorized message], and the [Keystone error rendering]. Wrong
credentials and unknown projects keep the `401` described below.

The token's user, project, and roles name the authenticated fixture. The
Compute and Block Storage catalog endpoints end with the token's project ID,
as Keystone substitutes `$(project_id)s` in endpoint URLs. See the
[Keystone catalog substitution].

Services keep the primary project as the only project with seeded resources:

- Block Storage refuses every operation for another project. Lists are
  empty, and creates and reads return `404`.
- Compute lists no flavors for another project, so it cannot boot servers.
  Server lists are empty, and the fixture server returns `404`. Key pairs
  belong to the user and server groups to the project.
- Image lists only images the project owns, and creates for another project
  are refused. A member project cannot read or accept an image shared with
  it.
- Networking records the owning project on each resource. Another project
  sees the external `public` network and its own resources and can create
  networks, subnets, ports, routers, and floating IPs. It has no default
  security group.

`SANDSTACK_LISTEN_ADDR` overrides the loopback listen address. Set
`SANDSTACK_PUBLIC_URL` to an externally reachable HTTP or HTTPS origin when
using a proxy or when the client-facing authority differs from the request
host. The catalog otherwise uses the request's host and HTTP scheme or TLS
state. `GET /_sandstack/health` and `GET /_sandstack/ready` return `204` when
the server is running.

## Supported operations

| Request | Behavior |
| --- | --- |
| `POST /identity/v3/auth/tokens` | Project-scoped password authentication or application credential authentication for any fixture user. A user or project can be selected by ID or by name with a domain. Success returns `201` and `X-Subject-Token`. Wrong credentials and another fixture's project return `401`. The `nocatalog` parameter omits the catalog from the token document. |
| `GET /identity/v3/auth/catalog` | Returns the public endpoints of the implemented services in `RegionOne` for a valid `X-Auth-Token`, with project-specific endpoints for the token's project. Missing, expired, and invalidated tokens receive `401`. |
| `HEAD /identity/v3/auth/catalog` | Returns the catalog request status and headers with an empty body. |
| `/identity/v3/users/{user_id}/application_credentials` | Creates, lists, reads, and deletes the token user's application credentials. See [Application credentials](identity-application-credentials.md). |
| `POST /_sandstack/identity/invalidate-tokens` | Requires `X-SandStack-Admin-Token`. Invalidates all tokens issued by that server, for every fixture, and returns `204`. Application credentials remain. Fresh authentication issues a usable token. |
| `POST /_sandstack/identity/invalidate-token` | Requires `X-SandStack-Admin-Token` and one nonempty `X-Subject-Token` header. Invalidates the selected token and returns `204`, including when the token is already absent. |

Use a versioned Identity URL. This scenario is limited to password and
application credential authentication, each fixture user's own project
scope, catalog reads, and the fixture users' application credentials.

After a successful catalog read, invalidate the sandbox's tokens to exercise
the client's recovery path.

```sh
curl --fail -X POST \
  -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" \
  http://127.0.0.1:8080/_sandstack/identity/invalidate-tokens
```

To exercise one client's recovery while keeping another session active, pass
the selected client's current token as `X-Subject-Token`.

```sh
curl --fail -X POST \
  -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" \
  -H "X-Subject-Token: ${SUBJECT_TOKEN}" \
  http://127.0.0.1:8080/_sandstack/identity/invalidate-token
```

Gophercloud clients with `AllowReauth` enabled obtain a fresh token and retry
the catalog request after receiving `401`. The other session keeps its
original token. See [Gophercloud's request handling].

## Verification

Run `go test ./...` to exercise the assembled server with raw HTTP and
Gophercloud v2.15.0. The client scenario authenticates, selects the catalog
endpoint, observes the failure after invalidation, and authenticates again.
The scoped control check also exercises automatic reauthentication and verifies
that the other client's original token still works. A second Gophercloud
client authenticates as an additional fixture and receives a distinct token,
project, and catalog, an empty view of the primary project's resources, and
its own application credentials. Raw HTTP tests check the additional
fixture's token body and the `401` body and `WWW-Authenticate` header for
each cross-project scope. Application tests check authentication by ID and
by name, the cross-project refusal, credential ownership, and token
invalidation across fixtures. The wire checks use the
[Identity v3 API reference] and the
[Keystone catalog handler] as sources. The catalog flow follows the
[Keystone default policy] for project-scoped tokens. The Keystone links are
pinned to commit `ffeba71ada0492f95c0e370b65d324005130000b`, the latest
`openstack/keystone` commit when additional fixtures were added.

[Identity v3 API reference]: https://docs.openstack.org/api-ref/identity/v3/
[Keystone catalog handler]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/api/auth.py#L236-L259
[Keystone default policy]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/common/policies/auth.py#L17-L26
[Keystone project scope check]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/models/token_model.py#L543-L550
[Keystone Unauthorized message]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/exception.py#L298-L317
[Keystone error rendering]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/server/flask/application.py#L150-L172
[Keystone catalog substitution]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/catalog/backends/sql.py#L276-L325
[Gophercloud's request handling]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/provider_client.go
