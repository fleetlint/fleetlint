# slop/huge-commits

Commits are reviewable

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

No commit in the recent history (`params.commits`, default 200) changes more than `params.max_lines` lines (default 1500).

## Why

A commit that large was not reviewed line by line, and cannot be reverted without taking unrelated changes with it.

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
item.lines <= params.max_lines
```

## Parameters

- `commits`: `200`
- `max_lines`: `1500`

## Fix

Split work into commits of one concern each.

Agent instruction: Commit one concern at a time. Do not rewrite pushed history without the user's decision.

See: docs/baseline.md#slop
