# deps/license-check

Dependency licenses are checked against an allowlist

| | |
|---|---|
| Severity | info |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | outcome |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

The task runner (any kind) or a CI workflow checks dependency licenses against an allowlist: go-licenses, pip-licenses, cargo deny, license-checker, licensee, osv-scanner with license scanning, REUSE, ScanCode, trivy, FOSSA or GitHub dependency review. Another checker is added with `accept:`.

## Why

A dependency under an incompatible license is found cheaply before it is merged and expensively after a release.

## Fix

Add the stack's license checker with an allowlist to the `audit` target (see docs/stacks.md).

Agent instruction: Add the stack's license checker to the audit target with an allowlist compatible with the repository's own license; report dependencies outside it instead of widening the list.

See: docs/baseline.md#dependencies
