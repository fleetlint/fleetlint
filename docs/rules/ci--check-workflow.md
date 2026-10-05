# ci/check-workflow

CI runs the check

| | |
|---|---|
| Severity | warning |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | outcome |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

A CI workflow runs the same `check` the hooks run, on pushes to main and on pull requests, whatever the task runner is. A repository whose check has another name adds it with `accept:`.

## Why

CI is the record that main was green; it must run the same gate as local, not a different subset.

## Fix

Add a check workflow for the stack; `fleetlint fix --apply` writes the template for the repository's task runner and container setting.

Agent instruction: Add .github/workflows/check.yml from the stack template (toolchain setup, shared tools, the task runner's check).

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/baseline.md#continuous-integration
