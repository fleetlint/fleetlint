# lint/shell

Shell scripts are linted

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

When the repository contains shell scripts, shellcheck runs as a hook, in the `lint` target or in CI.

## Why

Unquoted variables and unchecked exit codes are the bugs shell scripts ship with, and shellcheck finds them without running anything.

## Applies when

```cel
tracked().exists(p, p.endsWith(".sh"))
```

## Check

```cel
text(".pre-commit-config.yaml").contains("shellcheck") || text("Makefile").contains("shellcheck") || workflows().exists(w, text(w).contains("shellcheck"))
```

## Fix

Add the shellcheck hook to .pre-commit-config.yaml.

Agent instruction: Add the shellcheck hook (and shfmt) to .pre-commit-config.yaml and fix what it reports in the tracked scripts.

See: docs/baseline.md#static-analysis
