# release/semver-tags

Tags are semantic versions

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Every tag is `vMAJOR.MINOR.PATCH[-prerelease]`, optionally prefixed with a component name and slash in monorepos.

## Why

Release tooling, changelog generation and dependency resolvers all key on the pattern.

## Applies when

```cel
repo.has_git && tags().size() > 0
```

## Check

For each `item` in:

```cel
tags()
```

```cel
item.matches("^([A-Za-z0-9_.-]+/)?v?[0-9]+\\.[0-9]+\\.[0-9]+(-[0-9A-Za-z.-]+)?(\\+[0-9A-Za-z.-]+)?$")
```

## Fix

Delete or rename the offending tags; tag releases as vX.Y.Z from now on.

Agent instruction: List the offending tags for the user; do not delete tags without confirmation.
