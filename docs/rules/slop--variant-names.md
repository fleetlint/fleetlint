# slop/variant-names

No names with version or quality suffixes

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

No declaration is named as a variant of another (`parseV2`, `handlerNew`, `ClientImproved`, `syncOld`).

## Why

It means two implementations exist and callers must know which one is right.

## Check

For each `item` in:

```cel
codegrep("code", r'\b(func|fun|def|fn|function|class|struct|interface|type|void|const|final|let|var|val)\s+\w+?(V[2-9]|_v[2-9]|Improved|Enhanced|Old|New|Fixed|Temp|Simple|Robust|Better)\b')
```

```cel
false
```

## Fix

Change the original and update its callers; delete the other.

Agent instruction: Report the pair; merge into the original name only when the callers and tests make the intended behaviour clear.

See: docs/baseline.md#slop
