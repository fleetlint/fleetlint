# hooks/shared-hygiene

Hooks include the shared hygiene checks

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

The hook config includes the shared hygiene block (whitespace, final newline, merge markers, large files).

## Why

These catch the accidents no linter looks for, and the fixes are automatic.

## Applies when

```cel
file(".pre-commit-config.yaml")
```

## Check

```cel
yaml(".pre-commit-config.yaml").repos.exists(r,
  (r.repo == "builtin" || r.repo.contains("pre-commit-hooks"))
  && has(r.hooks)
  && ["trailing-whitespace", "end-of-file-fixer", "check-merge-conflict", "check-added-large-files"]
       .all(id, r.hooks.exists(h, h.id == id)))
```

## Fix

Add the `repo: builtin` block from templates/pre-commit-shared.yaml.

Agent instruction: Add the builtin hygiene hooks (trailing-whitespace, end-of-file-fixer, check-merge-conflict, check-added-large-files, detect-private-key) to .pre-commit-config.yaml.
