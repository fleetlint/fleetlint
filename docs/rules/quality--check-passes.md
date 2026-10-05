# quality/check-passes

The repository's own check passes within budget

| | |
|---|---|
| Severity | error |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | go |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

`make check` (or the task runner's equivalent) exits 0 and finishes within the budget (default 10 minutes). Evaluated only with `--deep`, because it runs the repository's code.

## Why

A gate that is red or slow gets bypassed. Measuring it is the only way to know it still works.

## Parameters

- `budget`: `10m`

## Fix

Run `make check` locally and fix what fails; if it is slow, cache or parallelize before adding more gates.

Agent instruction: Run `make check`, read the failing step's output, fix the cause (never the gate), re-run until green. If over budget, profile the slowest target and parallelize or cache it.
