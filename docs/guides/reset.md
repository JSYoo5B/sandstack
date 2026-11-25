# Reset the sandbox

Reset returns a running sandbox to its starting fixture state so the next
test case can use the same endpoint. It invalidates all issued Identity
tokens, restores the original fixture values and timestamps, removes
networks created by clients, and clears service faults, stale detail
snapshots, and the
[request journal](requests.md).

```sh
curl --fail -X POST -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" \
  http://127.0.0.1:8080/_sandstack/reset
```

The server returns `204` after the reset. The reset waits for in-flight
stateful requests to finish, and subsequent requests see the restored
fixtures. Authenticate again with the same fixture credentials. Repeating
reset restores the same fixtures and invalidates any tokens issued since the
previous reset.
