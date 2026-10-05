# changelog/generator-configured

Changelog generation is configured

| | |
|---|---|
| Severity | info |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

The changelog is generated from conventional commits (git-cliff or GoReleaser), then edited.

## Why

Hand-written changelogs drift from the commits; generated ones can be trusted and edited.

## Check

```cel
file("cliff.toml") || (file(".goreleaser.yaml") && has(yaml(".goreleaser.yaml").changelog))
```

## Fix

Add cliff.toml; `fleetlint fix --apply` writes the template.

Agent instruction: Add cliff.toml for git-cliff with Conventional Commits groups; `fleetlint fix --apply` writes the template.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).
