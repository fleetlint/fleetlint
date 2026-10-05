# slop/banner-comments

No banner or divider comments

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

No comment lines made of repeated symbols.

## Why

A file that needs dividers needs splitting.

## Check

For each `item` in:

```cel
codegrep("code", r'^\s*(//|#)\s*[─━═=\-*#~]{4,}')
```

```cel
false
```

## Fix

Delete the banner; split the file if the sections are separate concerns.

Agent instruction: Delete banner comments; do not restructure the file in the same change.

See: docs/baseline.md#slop
