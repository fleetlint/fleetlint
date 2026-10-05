# slop/typographic-chars

No typographic characters in source code

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

Source code uses ASCII quotes, hyphens and three dots, not curly quotes, long dashes, ellipsis characters or arrows.

## Why

They are invisible in review, break grep and are a trace of pasted prose.

## Check

For each `item` in:

```cel
codegrep("code", r'[—–“”‘’…→←⇒]')
```

```cel
false
```

## Fix

Replace with the ASCII equivalent.

Agent instruction: Replace with ASCII equivalents; keep characters that are user-facing text or test data.

See: docs/baseline.md#slop
