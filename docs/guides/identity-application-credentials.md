# Application credentials in the Identity sandbox

[Identity sandbox](identity.md)

Use application credentials to test a job that authenticates without a
fixture user's password. The job creates a credential with its
password-authenticated token, authenticates with the credential ID or name
and secret, and the credential can later be revoked. Credentials belong to
the fixture user who created them and live in memory for that server
instance. Each [additional fixture](identity.md#additional-fixtures) user
manages only their own credentials, and a token from a credential is scoped
to its owner's project.

## Create a credential

Use `applicationcredentials.Create` in Gophercloud v2.15.0. It sends
`POST /identity/v3/users/{user_id}/application_credentials` with an
`application_credential` object. Use the user ID from the token. Another
user ID returns `403`. See the [Application credentials reference] and the
[Keystone create handler].

The body accepts `name`, `description`, `secret`, `expires_at`, `roles`,
and `unrestricted`. Keystone ignores `id` and `project_id` in the body, and
so does SandStack. `name` is required and has 1 to 255 characters, at least
one of which is not whitespace. Each role names exactly one of `id` or
`name`. Unknown fields return `400`. See the [Keystone create schema].

Success returns `201` with `id`, `name`, `description`, `expires_at`,
`project_id`, `roles`, `unrestricted`, `links`, and `secret`. The ID is a
32-digit hexadecimal UUID. The secret appears only in this response, and
SandStack stores a salted PBKDF2-SHA512 hash of it. Without a secret, the
generated secret is 64 random bytes in unpadded URL-safe base64, which is
86 characters. SandStack draws the ID, secret, and salt from an injected
random source. See the [Keystone secret generation] and [Keystone secret
storage].

The credential takes its project from the token scope. Without `roles` it
receives the token's roles, which is the `member` role of every fixture. A role ID
that is not in the token returns `400`, and an unknown role name returns
`404`. `expires_at` is an ISO 8601 time in UTC, with or without a trailing
`Z`, such as `2026-12-31T00:00:00.000000`. A time that cannot be parsed or
that has already passed returns `400`. Responses write it in UTC with
microseconds and no zone, or `null` when the credential does not expire.
`description` and `unrestricted` are `null` when they are omitted. A name
the user already uses returns `409`. See the [Keystone role selection] and
[Keystone expiration parsing].

SandStack returns `400` for non-empty `access_rules` and for a non-null
`system`, because it does not enforce access rules or system scope.
Keystone's per-user credential limit is unset by default, and SandStack
applies none.

## List, read, and delete credentials

Use `applicationcredentials.List`, `applicationcredentials.Get`, and
`applicationcredentials.Delete`. `GET` and `HEAD` on the collection return
`200` with `application_credentials` in creation order and `links` with
`self`, and `previous` and `next` set to `null`. `ListOpts.Name` filters by
exact name. A user ID other than the token's user returns `403`.

`GET` and `HEAD` on a credential return `200` with the create fields
except `secret`, plus `user_id` and `system`, which is `null`. `DELETE`
returns `204`. As in Keystone, the owner check uses the stored
credential's user, so a user ID in the path is checked only when the
credential does not exist. A missing credential returns `404` for the
token's user and `403` for another user ID. See the [Keystone read and
delete handler].

## Authenticate with a credential

Set `ApplicationCredentialID` and `ApplicationCredentialSecret` in
`gophercloud.AuthOptions`, or set `ApplicationCredentialName` and the secret
with `UserID` or with `Username` and a domain. Do not set a scope.
`openstack.AuthenticatedClient` and `tokens.Create` send
`methods: ["application_credential"]`. See [Gophercloud's application
credential request] and the [Keystone authentication request].

Success returns `201` and `X-Subject-Token`. The token has
`methods: ["application_credential"]`, the credential's project, its
roles, the catalog unless `nocatalog` is set, and an
`application_credential` object with `id`, `name`, and `restricted`. The
token expires after one hour or at the credential's expiration, whichever
comes first. See the [Keystone token rendering] and the [Keystone token
expiration cap].

A wrong or missing secret, an expired credential, a requested scope, and an
unknown name return `401`. An unknown or deleted credential ID returns
`404`, because Keystone resolves the ID while it determines the token scope
and does not convert the missing-credential error. A name without a user
returns `400`. When the request has an ID, Keystone binds the token to the
credential's owner and ignores any `user` object. See the [Keystone
credential check], the [Keystone scope lookup], and the [Keystone
deleted-user test]. SandStack has not compared the `404` with a reference
deployment.

## Restricted credentials and revocation

A credential is restricted unless it was created with `unrestricted: true`.
A token issued from a restricted credential cannot create or delete
application credentials and receives `403`. It can still list and read
them. A token from an unrestricted credential can create one, which takes
the token's project and roles. See the [Keystone restriction check] and
the [Keystone recursion test].

Deleting a credential also invalidates the tokens issued from it. Keystone
resolves the credential each time it validates such a token, so the
tokens fail once the credential is gone. Requests with those tokens receive
`401` from every SandStack service. See the [Keystone token model].

`POST /_sandstack/identity/invalidate-tokens` and the single-token control
remove tokens but keep the credentials, so a client with `AllowReauth`
obtains a new token with the same credential. [Reset](reset.md) removes all
application credentials.

## Verification

Run `go test ./...` to create, list, read, and delete credentials through
Gophercloud and to authenticate with the credential ID and with its name.
The Gophercloud test also checks restricted and unrestricted tokens,
revocation after deletion, reauthentication after token invalidation, and
reset. Raw HTTP tests check the field sets, the status codes, request
validation, and the token body against the pinned Keystone source.
Application tests use a fixed clock and random stream to check the
generated ID, secret, and salt, the expiration boundary, and the token
expiration cap.

[Application credentials reference]: https://docs.openstack.org/api-ref/identity/v3/#application-credentials
[Keystone create handler]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/api/users.py#L769-L821
[Keystone create schema]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/application_credential/schema.py#L289-L335
[Keystone secret generation]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/api/users.py#L683-L690
[Keystone secret storage]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/application_credential/backends/sql.py#L150-L153
[Keystone role selection]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/api/users.py#L692-L739
[Keystone expiration parsing]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/common/utils.py#L361-L370
[Keystone read and delete handler]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/api/users.py#L182-L214
[Gophercloud's application credential request]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/auth_options.go#L219-L281
[Keystone authentication request]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/api-ref/source/v3/application-credentials.inc#L105-L177
[Keystone token rendering]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/common/render_token.py#L122-L136
[Keystone token expiration cap]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/token/provider.py#L335-L357
[Keystone credential check]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/application_credential/backends/sql.py#L132-L148
[Keystone scope lookup]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/auth/core.py#L232-L288
[Keystone deleted-user test]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/tests/unit/test_v3_auth.py#L6621-L6630
[Keystone restriction check]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/api/users.py#L87-L94
[Keystone recursion test]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/tests/unit/test_v3_application_credential.py#L214-L316
[Keystone token model]: https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/models/token_model.py#L191-L201
