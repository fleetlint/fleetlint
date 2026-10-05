# slop/placeholder-values

No placeholder values outside tests

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

Non-test code contains no `lorem ipsum`, `your-api-key`, `changeme` or `example.com` values.

## Why

A placeholder that ships is a broken feature or a default credential.

## Check

For each `item` in:

```cel
codegrep("nontest", r'(?i)lorem ipsum|your[-_ ]?(api[-_ ]?key|token|secret|password)|\bchange[-_]?me\b|\breplace[-_]?me\b|example\.(com|org)\b')
```

```cel
false
```

## Fix

Read the value from configuration and fail at startup when it is missing.

Agent instruction: Replace with a configuration lookup validated at startup; ask where the real value comes from if that is unclear.

See: docs/baseline.md#slop
