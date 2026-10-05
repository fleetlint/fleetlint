# slop/arbitrary-delays

No sleeps outside tests

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

Non-test code does not wait a fixed time for something to happen.

## Why

A delay that papers over an ordering problem fails on a slower machine and wastes time on a faster one.

## Check

For each `item` in:

```cel
codegrep("nontest", r'\b(sleep|Thread\.sleep|time\.Sleep|Future\.delayed|setTimeout|delay)\s*\(')
```

```cel
false
```

## Fix

Wait for the event or condition itself; keep real backoff and rate limiting with a comment saying so.

Agent instruction: Report each delay; replace it with waiting on the actual condition where that is evident, and leave deliberate backoff alone.

See: docs/baseline.md#slop
