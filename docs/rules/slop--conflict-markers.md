# slop/conflict-markers

No merge conflict markers in source

| | |
|---|---|
| Severity | error |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

No source file contains conflict markers.

## Why

The file does not mean what either side intended and usually does not compile.

## Check

For each `item` in:

```cel
codegrep("code", r'^(<{7}|>{7}|={7})( |$)')
```

```cel
false
```

## Fix

Resolve the conflict and remove the markers.

Agent instruction: Resolve the conflict using both sides' intent; ask when they contradict.

See: docs/baseline.md#slop
