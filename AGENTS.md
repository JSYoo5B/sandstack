# SandStack Agent Guidelines

## Decisions

Read the [decision index](docs/decisions/README.md) before changing code or
documentation. Revisit a discarded design only through a new decision record.
Before implementing a scenario, read the
[implementation scope](docs/decisions/0008-implementation-scope.md) and
[verification policy](docs/decisions/0009-verification-policy.md).

Follow the dependency direction in [ADR 0005](docs/decisions/0005-layered-architecture.md).
Keep Gophercloud at the API compatibility and test boundary described in
[ADR 0003](docs/decisions/0003-gophercloud-client-contract.md). Keep configuration
environment-based until it meets the conditions in
[ADR 0007](docs/decisions/0007-dependency-policy.md).

## Code and tests

Before placing new code or changing a package boundary, read
[Code placement](CONTRIBUTING.md#code-placement) and the ADRs it links to.
Before writing or changing tests, read
[Writing tests](docs/contributing/testing.md#writing-tests) for assertion,
test double, and regression guidance.

- Write durable project artifacts in English. Group files by responsibility
  and wrap long calls, literals, and assertions where it improves readability.
- Use Testify. Use `require` for prerequisites and `assert` for expected values.
  Use a suite when shared setup improves clarity. Separate setup, action, and
  assertions with whitespace and descriptive names.
- Prefer real implementations or small, explicit fakes. Use a mock when the
  substituted behavior and what the test checks are clear at the call site.
  Keep doubles small enough to review without reimplementing external behavior.
- If a reported defect leaves its regression test green, check that the test
  fails when the defect is present before relying on it.

## Changes and verification

Before pushing changes or publishing results, read
[Documentation and review](CONTRIBUTING.md#documentation-and-review) and
[Reporting results](docs/contributing/testing.md#reporting-results).

- Develop on `main` and push verified commits directly. Use a separate branch
  or pull request when the maintainer requests one.
- For documentation changes, review the diff and check affected links. Report
  compilation, tests with substitutes, Gophercloud compatibility, and checks
  against OpenStack according to what actually ran. Tie CI results to the
  commit and run attempt that produced them.
- Before publishing, check the diff and any attached logs for secrets, private
  hostnames, or account-specific paths. Use repository-relative paths and
  retain only diagnostic details needed to explain the result.
- Keep documentation focused on current decisions, behavior, and usage.
  Record change rationale and verification in the relevant decision record,
  issue, or pull request.
