# Reset the sandbox

Reset returns a running sandbox to its starting fixture state so the next
test case can use the same endpoint. It invalidates all issued Identity
tokens and restores the original fixture values and timestamps. It also
removes [application credentials](identity-application-credentials.md),
[Nova servers](compute-servers.md), Nova key pairs, Glance image members,
networks, subnets, ports, routers, volumes, volume snapshots, and volume
backups created by clients, including pending backup creations and
restores. Reset also restores a deleted fixture server, stops holding
server builds and [server snapshots](compute-snapshots.md), and clears volume attachments, server locks,
[server action history](compute-actions.md) except the fixture `create`
record, replaced [console output](compute-console.md), service faults,
stale detail snapshots, and the [request journal](requests.md).

```sh
curl --fail -X POST -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" \
  http://127.0.0.1:8080/_sandstack/reset
```

The server returns `204` after the reset. The reset waits for in-flight
stateful requests to finish, and subsequent requests see the restored
fixtures. Authenticate again with the same fixture credentials. Repeating
reset restores the same fixtures and invalidates any tokens issued since the
previous reset.
