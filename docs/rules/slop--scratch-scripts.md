# slop/scratch-scripts

No scratch scripts at the repository root

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

No one-off scripts such as `fix_imports.py`, `debug-auth.js` or `tmp_check.sh` are tracked at the root.

## Why

They were written to answer one question once; nobody knows later whether they are safe to run or to delete.

## Check

For each `item` in:

```cel
tracked().filter(p, p.matches(r'^(test|fix|debug|temp|tmp|scratch|try|quick)[_-][^/]*\.(py|js|ts|sh|go|dart|kt|rs)$'))
```

```cel
false
```

## Fix

Delete the script, or move it into a tools directory with a name that says what it does.

Agent instruction: Report the script; delete it only if nothing references it, otherwise move it under scripts/ with a descriptive name.

See: docs/baseline.md#slop
