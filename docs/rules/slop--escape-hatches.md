# slop/escape-hatches

Lint and type suppressions are reviewed

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

Suppressions (`nolint`, `noqa`, `@ts-ignore`, `as any`, `@Suppress`, `unwrap()`, `!!`) are rare and each has a reason next to it. This rule lists them for review; it cannot tell a justified one from an unjustified one.

## Why

Each suppression switches a check off for good; a list of them is the fastest audit of where the checks do not apply.

## Check

For each `item` in:

```cel
codegrep("nontest", r'@ts-ignore|@ts-nocheck|\bas any\b|:\s*any\s*([,;)=\]>]|$)|# type:\s*ignore|eslint-disable|//\s*nolint|#\s*noqa|@Suppress(Warnings)?\(|//\s*ignore(_for_file)?:|#!?\[allow\(|\.unwrap\(\)|\.expect\(\x22|\w!!')
```

```cel
false
```

## Fix

Fix the code instead of suppressing, or state the reason on the same line.

Agent instruction: For each suppression, try the fix first; if it stays, add the specific rule name and a one-line reason.

See: docs/baseline.md#slop
