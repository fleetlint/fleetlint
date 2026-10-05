# deps/update-automation

Dependency updates are automated

| | |
|---|---|
| Severity | warning |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Renovate (preferred, works on GitHub and Gitea) or Dependabot keeps dependencies and pinned actions current.

## Why

Unpinned is unsafe and pinned-but-stale is unsafe too; only automation keeps both true.

## Check

```cel
exists("renovate.json*") || exists(".renovaterc*") || exists(".github/renovate.json*") || file(".github/dependabot.yml") || file(".gitea/dependabot.yml") || file(".gitlab/renovate.json")
```

## Fix

Add renovate.json extending config:recommended with the pinned-actions preset.

Agent instruction: Add renovate.json: {"$schema": "https://docs.renovatebot.com/renovate-schema.json", "extends": ["config:recommended", "helpers:pinGitHubActionDigests"]}.

See: docs/baseline.md#dependencies
