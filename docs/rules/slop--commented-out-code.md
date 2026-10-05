# slop/commented-out-code

No commented-out code

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

No code is kept in comments.

## Why

Version control keeps old code; commented-out code is never updated and nobody dares delete it.

## Check

For each `item` in:

```cel
codegrep("code", r'^\s*(//|#)\s*(if|for|while|return|await|const|let|var|final|val|def|func|fn|import|from)\b.*([;{)(]|:)\s*$')
```

```cel
false
```

## Fix

Delete it.

Agent instruction: Delete commented-out code.

See: docs/baseline.md#slop
