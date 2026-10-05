# release/changelog-in-release

Release notes are generated from commits

| | |
|---|---|
| Severity | info |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | outcome |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Release notes come from git-cliff, GoReleaser's changelog, release-please or changesets, so they match the CHANGELOG.

## Why

Hand-written release notes drift from the changelog and from what was shipped.

## Applies when

```cel
release.exists && tagged_workflows().size() > 0
```

## Fix

Use git-cliff --latest --strip header as the release body.

Agent instruction: Add a step that runs `git-cliff --latest --strip header -o release-notes.md` and pass it as the release body.
