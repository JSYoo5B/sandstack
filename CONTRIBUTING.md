# Contributing to SandStack

Start with the [decision index](docs/decisions/README.md). Add or supersede an
ADR when a design decision changes. The contributor guides explain how to
work within those decisions.

## Code placement

- Start with the OpenStack service whose API changes. Keep its routes, wire
  DTOs, and response mapping in that service's API package. Apply the
  [Gophercloud boundary](docs/decisions/0003-gophercloud-client-contract.md)
  when choosing wire and domain types.
- Use the [layer responsibilities](docs/decisions/0005-layered-architecture.md)
  to place orchestration, lifecycle rules, persistence, and runtime backends.
  Keep service-specific changes in their owning packages. Add a layer when
  the change needs it.
- Group files by responsibility and wrap long calls, literals, and assertions
  where it improves readability. Follow the
  [dependency policy](docs/decisions/0007-dependency-policy.md) for configuration
  and new dependencies.

## Documentation and review

- Keep design decisions in `docs/decisions/`, contributor procedures in
  `docs/contributing/`, and usage guides in `docs/guides/`. Link to the
  document that owns the rule. Put work in
  progress, change rationale, and verification results in the corresponding
  issue or pull request. Keep the README focused on the project and how to
  get started.
- Keep related changes together in one pull request. Use the repository's
  issue or pull request template when it applies, including submissions
  through the CLI or API.
- Before publishing, check changes and attached logs for secrets, private
  hostnames, or account-specific paths. Use repository-relative paths and
  keep the diagnostic details needed to explain the result.
- For documentation changes, review the diff and check affected links. Use
  the [testing guide](docs/contributing/testing.md) to report other checks.
