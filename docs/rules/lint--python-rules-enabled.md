# lint/python-rules-enabled

ruff selects the baseline rule families

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | python |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

`[tool.ruff.lint] select` in pyproject.toml contains every family in `params.required` (pycodestyle, pyflakes, isort, bugbear, pyupgrade, simplify, blind-except, bandit, complexity, print), or `ALL`.

## Why

ruff's default selection is a small subset; blind excepts, security findings and leftover prints are only reported when their families are selected.

## Applies when

```cel
file("pyproject.toml") && has(toml("pyproject.toml").tool) && has(toml("pyproject.toml").tool.ruff)
```

## Check

For each `item` in:

```cel
params.required
```

```cel
(has(toml("pyproject.toml").tool.ruff.lint) && has(toml("pyproject.toml").tool.ruff.lint.select)
  && (item in toml("pyproject.toml").tool.ruff.lint.select || "ALL" in toml("pyproject.toml").tool.ruff.lint.select))
|| (has(toml("pyproject.toml").tool.ruff.select)
  && (item in toml("pyproject.toml").tool.ruff.select || "ALL" in toml("pyproject.toml").tool.ruff.select))
```

## Parameters

- `required`: `[E F I B UP SIM BLE S C90 T20]`

## Fix

Add the missing families to `select` under [tool.ruff.lint].

Agent instruction: Add each reported family to [tool.ruff.lint] select; ratchet the violations that appear with `ruff check --add-noqa` instead of fixing them in the same change.

See: docs/stacks.md#python
