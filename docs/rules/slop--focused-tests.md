# slop/focused-tests

No focused tests are committed

| | |
|---|---|
| Severity | error |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

No test is marked to run alone (`fdescribe`, `fit`, `.only`, `solo: true`).

## Why

A focused test silently disables every other test in the suite while CI stays green.

## Check

For each `item` in:

```cel
codegrep("code", r'\b(fdescribe|fit|describe\.only|it\.only|test\.only)\(|\bsolo:\s*true')
```

```cel
false
```

## Fix

Remove the focus marker.

Agent instruction: Remove the focus marker and run the whole suite.

See: docs/baseline.md#slop
