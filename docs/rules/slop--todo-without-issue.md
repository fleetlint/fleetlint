# slop/todo-without-issue

TODOs reference an issue

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

Every TODO, FIXME, XXX or HACK comment names an issue: `TODO(#123)` or `TODO(ABC-123)`.

## Why

An untracked TODO is a wish; a tracked one has an owner and can be closed.

## Check

For each `item` in:

```cel
codegrep("code", r'(//|#|/\*).*\b(TODO|FIXME|XXX|HACK)\b')
```

```cel
item.text.matches(r'\b(TODO|FIXME|XXX|HACK)\b\s*\((#|[A-Z][A-Z0-9]+-)\d+\)')
```

## Fix

Open an issue and reference it, or do the work, or delete the note.

Agent instruction: Report each note; do not invent issue numbers.

See: docs/baseline.md#slop
