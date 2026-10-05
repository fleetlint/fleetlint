# slop/vague-commit-messages

Commit subjects say what changed

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

No commit in the recent history (`params.commits`, default 200) has a subject that is only `fix`, `update`, `wip`, `changes`, `misc` or `cleanup`.

## Why

The history is the record of why the code changed; a one-word subject records nothing.

## Applies when

```cel
repo.has_git
```

## Check

For each `item` in:

```cel
commits(params.commits)
```

```cel
!item.subject.matches(r'(?i)^(fix(es)?|update[sd]?|wip|changes?|misc|stuff|tmp|test|minor|cleanup|\.)\.?$')
```

## Parameters

- `commits`: `200`

## Fix

Write the subject from the diff; squash work-in-progress commits before merging.

Agent instruction: Write subjects from the diff. Do not rewrite pushed history without the user's decision.

See: docs/baseline.md#slop
