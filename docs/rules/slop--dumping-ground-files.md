# slop/dumping-ground-files

No utils or helpers dumping grounds

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

No source file is named `utils`, `helpers`, `common` or `misc`; a function lives in the module that owns its concept.

## Why

A file named after nothing collects everything, and everything then depends on it.

## Check

For each `item` in:

```cel
tracked().filter(p, p.matches(r'(^|/)(utils?|helpers?|common|misc|stuff|shared_utils)\.[a-z]+$'))
```

```cel
false
```

## Fix

Move each function to the module it belongs to.

Agent instruction: Report the file with its functions grouped by concept; move them only as a separate, behaviour-neutral change.

See: docs/baseline.md#slop
