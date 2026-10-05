# hooks/config-present

Git hooks are configured

| | |
|---|---|
| Severity | error |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-minimal@0.1.0 |

## Requirement

The repository declares pre-commit hooks that run on every commit.

## Why

Hooks are the only gate that runs before code reaches the remote; everything else is after the fact.

## Check

```cel
file(".pre-commit-config.yaml") || file("lefthook.yml") || file(".lefthook.yml")
```

## Fix

Run `fleetlint fix --apply` to add .pre-commit-config.yaml for the stack, then `prek install`.

Agent instruction: Create .pre-commit-config.yaml from the shared template plus the stack block, run `prek update` and `prek install`.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/baseline.md#git-hooks
