# quality/coverage-threshold

A coverage floor is enforced

| | |
|---|---|
| Severity | warning |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | outcome |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

The `cover` target (or the CI coverage tool) fails below a floor: a total or diff-coverage threshold in the Makefile recipe, codecov `target`, pytest `--cov-fail-under`, Jest/Vitest thresholds, Kover/JaCoCo verification rules, or `cargo llvm-cov --fail-under`.

## Why

A coverage number nobody enforces drifts down one "quick fix" at a time.

## Fix

Make `make cover` fail below the floor (diff-cover --fail-under=80 is the stack-neutral option).

Agent instruction: Add a threshold to the cover recipe using the stack profile's tool (go: cover.sh with a minimum; python: --cov-fail-under; node: coverageThreshold; rust: cargo llvm-cov --fail-under; kotlin: kover verify).

See: docs/baseline.md#tests
