# lint/python-config

ruff and a strict type checker are configured

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | python |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

pyproject.toml configures ruff with a broad rule selection and mypy --strict or (based)pyright strict.

## Why

Untyped Python with default linting is where AI-written code hides most bugs.

## Applies when

```cel
file("pyproject.toml")
```

## Check

```cel
has(toml("pyproject.toml").tool.ruff) && (has(toml("pyproject.toml").tool.mypy) || has(toml("pyproject.toml").tool.pyright) || has(toml("pyproject.toml").tool.basedpyright))
```

## Fix

Add [tool.ruff] and [tool.mypy] sections from the Python stack profile.

Agent instruction: Add [tool.ruff] with the profile's rule selection and [tool.mypy] strict = true to pyproject.toml; ratchet existing violations with `ruff check --add-noqa`.

See: docs/stacks.md#python
