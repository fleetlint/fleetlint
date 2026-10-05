# ci/check-workflow

CI runs the check

| | |
|---|---|
| Severity | warning |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

A CI workflow runs the same `check` the hooks run, on pushes to main and on pull requests.

## Why

CI is the record that main was green; it must run the same gate as local, not a different subset.

## Check

```cel
glob(".github/workflows/*.y*ml").exists(w, text(w).contains("make check")) || glob(".gitea/workflows/*.y*ml").exists(w, text(w).contains("make check")) || glob(".github/workflows/*.y*ml").exists(w, text(w).contains("gradlew check") || text(w).contains("npm run check"))
```

## Fix

Add a check workflow for the stack; `fleetlint fix --apply` writes the template.

Agent instruction: Add .github/workflows/check.yml from the stack template (toolchain setup, shared tools, make check).

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/baseline.md#continuous-integration
