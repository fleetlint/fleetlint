# repo/lockfile-committed

Dependency lockfile is committed

| | |
|---|---|
| Severity | error |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | each |
| Kind | go |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Every stack's lockfile (go.sum, uv.lock/poetry.lock, Cargo.lock, package-lock.json/pnpm-lock.yaml/yarn.lock, pubspec.lock, gradle.lockfile) is tracked.

## Why

Builds are reproducible only when dependency versions are pinned in git.

## Fix

Generate the lockfile with the package manager and commit it; remove it from .gitignore.

Agent instruction: Run the package manager to produce the lockfile, remove any .gitignore entry for it, and commit it.

See: docs/baseline.md#dependencies
