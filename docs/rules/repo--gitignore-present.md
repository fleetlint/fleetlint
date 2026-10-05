# repo/gitignore-present

A .gitignore exists

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-minimal@0.1.0 |

## Requirement

The repository root has a .gitignore.

## Why

Without one, build output and local files end up tracked.

## Check

```cel
file(".gitignore")
```

## Fix

Add a .gitignore for the stack (gitignore.io or the stack template).

Agent instruction: Create .gitignore with the standard patterns for the detected stacks and OS/editor files.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).
