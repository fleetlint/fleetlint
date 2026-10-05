# quality/policy-enforced

fleetlint runs automatically

| | |
|---|---|
| Severity | warning |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

`fleetlint check` runs without anyone remembering to: as a git hook, in the task runner's `check` target, or in a CI workflow.

## Why

A policy that is only checked by hand drifts with the first busy week; the report is then a description of last month.

## Check

```cel
text(".pre-commit-config.yaml").contains("fleetlint") || text("lefthook.yml").contains("fleetlint") || [taskrunner.file, "Makefile", "justfile", "Taskfile.yml", "package.json", "build.gradle", "build.gradle.kts"].exists(f, text(f).contains("fleetlint")) || workflows().exists(w, text(w).contains("fleetlint"))
```

## Fix

Add `fleetlint check --fail-on error` to the `check` target, and the fleetlint hook to .pre-commit-config.yaml (see docs/integrations.md).

Agent instruction: Add `fleetlint check --fail-on error` as the last step of the task runner's check target and a local fleetlint hook to .pre-commit-config.yaml; make sure CI installs fleetlint before it runs the check.

See: docs/integrations.md
