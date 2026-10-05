# slop/swallowed-errors

Errors are not swallowed

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

No empty catch blocks, no `except: pass`, no `_ = err`. Only single-line forms are detected; the stack's linter covers the rest.

## Why

A swallowed error turns a failure with a cause into a wrong result without one.

## Check

For each `item` in:

```cel
codegrep("nontest", r'catch\s*(\([^)]*\))?\s*\{\s*\}|except[^:]*:\s*pass\b|\b_\s*=\s*err\b|\.catch\(\s*\(\)\s*=>\s*(\{\s*\}|null|undefined)\s*\)|catch \(_\)')
```

```cel
false
```

## Fix

Handle the error, wrap and return it, or log it where it is handled.

Agent instruction: Propagate the error with context unless the surrounding code shows it is safe to ignore; then say why in a comment.

See: docs/baseline.md#slop
