# taskrunner/targets

Task runner exposes the contract targets

| | |
|---|---|
| Severity | error |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | go |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

The task runner (Makefile, or package.json scripts / Gradle where that is the stack's convention) exposes fmt, lint, test, cover, audit, check and check-fast, so every tool and person can run the same commands in every repository.

## Why

Hooks, CI and the agent rules all depend on `make check` meaning the same thing everywhere.

## Parameters

- `required`: `[fmt lint test cover audit check check-fast]`

## Fix

Add the missing targets; `fleetlint fix --apply` writes a Makefile template when there is none.

Agent instruction: Add the missing targets to the Makefile following the template (SHELL bash -euo pipefail, .PHONY, help target). Fill each with the stack's real command, no TODO placeholders in check-fast or check.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/baseline.md#task-runner-contract
