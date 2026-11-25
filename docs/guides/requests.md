# Inspect recent requests

Use the request journal to check which OpenStack operations a client
attempted and how the sandbox responded. Typical checks include a `401`
followed by a new token request and a successful retry.

```sh
curl --fail -H "X-SandStack-Admin-Token: ${SANDSTACK_ADMIN_TOKEN}" \
  http://127.0.0.1:8080/_sandstack/requests
```

The response contains a `requests` array. Each record has `sequence`,
`method`, `route`, and HTTP `status`. The journal retains the last 128
completed requests that match a supported OpenStack route. Entries appear
from oldest to newest. An empty journal returns `{"requests":[]}`.

The route is a registered template such as
`/compute/v2.1/{projectID}/servers/{serverID}`. Records contain the template,
method, status, and sequence. Health checks and SandStack controls leave
the journal untouched. [Reset the sandbox](reset.md) to clear the journal
and restart the sequence at one.
