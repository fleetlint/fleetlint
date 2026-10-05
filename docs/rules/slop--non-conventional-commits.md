# slop/non-conventional-commits

Commit subjects follow Conventional Commits

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

Commit subjects in the recent history (`params.commits`, default 200) have the form `type(scope): subject`; merge commits are exempt.

## Why

The changelog and the next version number are computed from the subjects.

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
item.subject.matches(r'^(feat|fix|refactor|perf|test|docs|build|chore|revert|ci|style)(\([^)]+\))?!?: ') || item.subject.startsWith("Merge ")
```

## Parameters

- `commits`: `200`

## Fix

Install the commit-msg hook so new commits are checked.

Agent instruction: Use Conventional Commits for new commits. Do not rewrite pushed history without the user's decision.

See: docs/baseline.md#slop
