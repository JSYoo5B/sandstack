# Reset the sandbox

Reset returns a running sandbox to its starting fixture state so the next
test case can use the same endpoint. It invalidates all issued Identity
tokens, restores the Compute fixture and its original timestamps, and
clears the instance fault and stale detail snapshot.

```sh
curl --fail -X POST -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" \
  http://127.0.0.1:8080/_sandstack/reset
```

The server returns `204` after the reset. The reset waits for in-flight
stateful requests to finish, and subsequent requests see the restored
fixtures. Authenticate again with the same fixture credentials. Repeating
reset restores the same fixtures and invalidates any tokens issued since the
previous reset.
