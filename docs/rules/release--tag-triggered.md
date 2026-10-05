# release/tag-triggered

Releases run from a tag-triggered workflow

| | |
|---|---|
| Severity | error |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Releases are produced by CI from a `v*` tag, never from a laptop.

## Why

A release built locally has no provenance and cannot be reproduced.

## Applies when

```cel
release.mechanisms.exists(m, m != "git-cliff")
```

## Check

```cel
tagged_workflows().size() > 0
```

## Fix

Add release.yml triggered on push.tags ['v*'] that runs the release tool; `fleetlint fix --apply` writes the stack's template (GoReleaser for Go and Rust, `make dist` plus syft and cosign otherwise) as a starting point.

Agent instruction: Add .github/workflows/release.yml (or .gitea/workflows/) with `on: push: tags: ['v*']` running the existing release tool; keep permissions minimal. Adapt the template to the release tool already in the repository instead of replacing it.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/baseline.md#releases
