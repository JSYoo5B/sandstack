# Testing

## Writing tests

Use Testify. Use `require` for setup and prerequisites and `assert` for
expected values. Use a suite when shared setup improves clarity. Use
whitespace and descriptive names to separate setup, action, and assertions.

Prefer real implementations or small, explicit fakes. Use a mock when the
test makes the substituted behavior and the assertion clear at the call
site. Keep doubles small and focused on the behavior the test needs.

When a reported defect leaves its regression test green, check that the
test reaches the affected behavior and fails with the defect present.
Remove any temporary fault used to check the test and retain the regression
assertion.

## Reporting results

Report what ran and what it established. Identify compilation, tests using
substitutes, Gophercloud client checks, and checks against OpenStack by the
environment and result of each check. See the
[Gophercloud contract](../decisions/0003-gophercloud-client-contract.md) for
the scope of client compatibility. Cite the commit and run attempt for CI
results.

The [decision index](../decisions/README.md#pending) tracks the pending
verification policy, including what evidence will be required for
implementation changes.
