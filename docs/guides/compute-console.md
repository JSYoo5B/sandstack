# Console output in the Compute sandbox

[Compute sandbox](compute.md)

Use the Nova v2.1 console output action to read the fixture server's
console log, such as a client that waits for a boot or login line.

## Read the console log

Use `servers.ShowConsoleOutput` in Gophercloud v2.15.0 with the fixture
server ID. It sends `POST /servers/{server_id}/action` with
`{"os-getConsoleOutput":{}}`, or with `{"length":N}` when
`ShowConsoleOutputOpts.Length` is positive. Nova returns `200` with
`{"output":"..."}`. Gophercloud omits a zero length, so it cannot request
`0` or `-1`. See the [Nova console output reference], [Nova console output
controller], and [Gophercloud console output].

The fixture log has four lines and no trailing newline:

```text
[    0.000000] Linux version 6.8.0-sandstack (fixture@sandstack) #1 SMP
[    1.000000] Run /sbin/init as init process
cloud-init: modules:final finished
fixture-server login: 
```

The last line ends with a space after the colon. The project member role
of the fixture user may read the log. See the [Nova console output policy].

## Select the last lines

Nova splits the log on `\n` and returns the last `length` segments joined
with `\n`. A log that ends in a newline has an empty final segment, so
`length` `1` returns an empty string and `2` returns the last line with
its newline. A length larger than the log returns the whole log. `0`
returns an empty string. An omitted or `null` length returns the whole
log. See the [Nova console tail].

The length is an integer, a string of decimal digits with an optional
leading `-`, or `null`. An integral JSON number such as `3.0` is accepted.
Values below `-1`, including strings such as `"-2"`, return `400`. A
fraction, a boolean, another field, or another action key in the same
body returns `400`. See the [Nova console output schema] and [Nova numeric
string minimum].

The schema notes that `-1` means an unlimited length, but Nova does not
convert it. The compute manager slices from the second segment, so `-1`
returns the log without its first line. SandStack follows the source. No
reference deployment has confirmed this case.

Nova decodes the log as ASCII and replaces each non-ASCII byte with
U+FFFD. It then removes control characters from `0x00` to `0x1F` except
tab and newline, so carriage returns are removed. Line selection happens
before removal. See the [Nova console tail] and [Nova console output
controller].

## Errors and server state

An unknown server returns `404` after body validation. Nova reads the log
without lock or state checks and returns `409` only when the server has no
compute host. The fixture server always has a host, so the log is readable
while it is stopped, paused, suspended, locked, or in an injected `ERROR`.
SandStack does not model `404` for a missing log or `501` for a driver
without console support. Reading the log records no
[server action](compute-actions.md). See the [Nova console host check].

## Replace the console log

Use the `console-output` control with the SandStack admin token to set the
log that the next reads return. The request body is the raw log, up to
1 MiB, in any encoding.

```sh
server_id=d8ba25c2-8224-4e09-88ef-fab16c654770
console_url="http://127.0.0.1:8080/_sandstack/compute/servers/${server_id}/console-output"
printf 'booting\nready\n' | curl --fail -X PUT --data-binary @- \
  -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" "$console_url"
```

The control returns `204`. Later reads apply the same line selection,
decoding, and character removal to the new log. An empty body sets an
empty log. Send `DELETE` to the same URL to restore the fixture log. An
unknown server ID returns `404`, and a larger body returns `413`.
[Reset the sandbox](reset.md) to restore the fixture log as well.

## Verification

The SDK test reads the whole fixture log and its last two lines, replaces
the log, and reads it again while the server is stopped and locked. It
also checks that no action is recorded, that `DELETE` and reset restore
the fixture log, and that an unknown server returns `404`. Raw HTTP checks
the length schema, line selection including `0` and `-1`, character
removal, and the control's error statuses against the pinned Nova source.

[Nova console output reference]: https://docs.openstack.org/api-ref/compute/#show-console-output-os-getconsoleoutput-action
[Nova console output controller]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/console_output.py#L36-L73
[Gophercloud console output]: https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servers/requests.go#L1095-L1125
[Nova console output policy]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/policies/console_output.py#L25-L35
[Nova console tail]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L7561-L7584
[Nova console output schema]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/console_output.py#L15-L36
[Nova numeric string minimum]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/validation/validators.py#L258-L340
[Nova console host check]: https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5040-L5044
