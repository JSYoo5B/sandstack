# Key pairs in the Compute sandbox

[Compute sandbox](compute.md)

Use the project token from the [Identity fixture](identity.md) to manage
the fixture user's SSH key pairs with Nova v2.1. Key pairs belong to the
authenticated user, not to the project. SandStack does not create servers,
so a key pair cannot be injected into one.

## Create or import a key pair

Use `keypairs.Create` in Gophercloud v2.15.0. It sends
`POST /os-keypairs` with a `keypair` object. Nova v2.1 returns `200` with
`name`, `public_key`, `fingerprint`, and `user_id`. See the [Nova key pair
reference], [Nova key pair create], and [Gophercloud key pair operations].

Set `keypairs.CreateOpts.PublicKey` to import an OpenSSH public key such as
`ssh-rsa AAAA... comment`. SandStack accepts `ssh-rsa`, `ssh-dss`,
`ssh-ed25519`, and `ecdsa-sha2-nistp256`, `-nistp384`, or `-nistp521`
keys. The fingerprint is the MD5 digest of the decoded key, written as 16
colon-separated hex pairs. A malformed key, a mismatched type, or trailing
data returns `400`. See the [Nova fingerprint].

SandStack expects the key type, key data, and optional comment separated
by single spaces, with padded base64 key data. Nova validates keys with
the Python `cryptography` library, whose accepted key formats vary by
release. SandStack returns `400` for any other key type or layout.

Omit `PublicKey` to have SandStack generate a key pair. The response adds
`private_key`, a PEM-encoded PKCS #1 RSA key. The public key is the
matching 2048-bit `ssh-rsa` key with the comment `Generated-by-Nova`, as
Nova generates it. SandStack draws the key from an injected random source.
The private key appears only in the create response. See the [Nova key
generation].

A name has 1 to 255 ASCII letters, digits, spaces, `-`, or `_`, with no
leading or trailing space. The body accepts only `name` and `public_key`.
`type` and `user_id` require later microversions and return `400` at v2.1,
so leave `CreateOpts.Type` and `CreateOpts.UserID` empty. A name already
used by the same user returns `409`. Each user can hold Nova's default
quota of 100 key pairs. Creating one more returns `403` before the key is
checked. See the [Nova key pair schema], [Nova key pair name], [Nova key
pair quota], and [Nova key pair conflict].

## List, read, and delete key pairs

Use `keypairs.List`, `keypairs.Get`, and `keypairs.Delete` with
`GET /os-keypairs`, `GET /os-keypairs/{name}`, and
`DELETE /os-keypairs/{name}`. Encode spaces in the name in the URL. The
list returns `200` with each item wrapped in a `keypair` object that holds
`name`, `public_key`, and `fingerprint`, sorted by name. At v2.1 the list
has no pagination and the query is ignored, including `user_id`. See the
[Nova key pair views].

`keypairs.Get` returns `200` with `user_id`, an integer `id`, and
`created_at` in UTC with microseconds and no zone, such as
`2026-11-28T01:02:03.000000`. `deleted` is `false`, and `updated_at` and
`deleted_at` are `null`. Gophercloud reads the public fields. Delete
returns `202` with an empty body. A missing name returns `404` on read or
delete. See the [Nova key pair read and delete].

SandStack compares names exactly. Nova stores key pairs in its database,
where the collation can make name comparisons ignore case. Reset removes
all key pairs and restarts `id` at `1`.

## Verification

Run `go test ./...` to check creation, import, listing, reading, deletion,
and reset through Gophercloud. Raw HTTP tests check the v2.1 field sets,
the `200` and `202` status codes, request validation, conflicts, quota,
and `404` responses against the pinned Nova source. Unit tests check the
fingerprints against Nova's crypto test vectors and that the same random
stream generates the same key pair.

[Nova key pair reference]: https://docs.openstack.org/api-ref/compute/#import-or-create-keypair
[Nova key pair create]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/keypairs.py#L44-L112
[Gophercloud key pair operations]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/keypairs/requests.go#L76-L185
[Nova fingerprint]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/crypto.py#L68-L81
[Nova key generation]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/crypto.py#L100-L107
[Nova key pair schema]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/keypairs.py#L21-L36
[Nova key pair name]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/validation/parameter_types.py#L298-L308
[Nova key pair quota]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L7000-L7009
[Nova key pair conflict]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/objects/keypair.py#L69-L76
[Nova key pair views]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/views/keypairs.py#L24-L74
[Nova key pair read and delete]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/keypairs.py#L119-L175
