# changelog/present

CHANGELOG.md exists

| | |
|---|---|
| Severity | warning |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

The repository keeps a CHANGELOG.md in Keep a Changelog format.

## Why

Users and future you need to know what changed between versions without reading commits.

## Check

```cel
file("CHANGELOG.md")
```

## Fix

Add cliff.toml and generate CHANGELOG.md with git-cliff.

Agent instruction: Add cliff.toml from the template and run `git-cliff -o CHANGELOG.md`.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).
