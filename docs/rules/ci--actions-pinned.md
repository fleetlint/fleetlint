# ci/actions-pinned

Actions are pinned to commit SHAs

| | |
|---|---|
| Severity | warning |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | go |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Every `uses:` reference in a workflow is pinned to a full 40-character commit SHA with a version comment.

## Why

A tag can be moved; a SHA cannot. Pinning is what makes a workflow reproducible and tamper-evident.

## Fix

Run `pinact run` and commit; let Renovate keep the pins current.

Agent instruction: Run `pinact run` to pin all uses: references to SHAs with version comments; do not change anything else in the workflows.

See: docs/baseline.md#security
