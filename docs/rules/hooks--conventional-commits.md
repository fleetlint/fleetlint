# hooks/conventional-commits

Commit messages are checked

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

A commit-msg hook rejects messages that are not conventional commits.

## Why

The changelog and version bump are generated from commit types; one bad message breaks both.

## Applies when

```cel
file(".pre-commit-config.yaml")
```

## Check

```cel
yaml(".pre-commit-config.yaml").repos.exists(r,
  r.repo.contains("conventional") || (has(r.hooks) && r.hooks.exists(h, h.id.contains("commit"))))
```

## Fix

Add the conventional-pre-commit hook with stages: [commit-msg].

Agent instruction: Add compilerla/conventional-pre-commit to .pre-commit-config.yaml with stages: [commit-msg] and the allowed types.
