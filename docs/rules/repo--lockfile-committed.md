# repo/lockfile-committed

Dependency lockfile is committed

| | |
|---|---|
| Severity | error |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Every stack's lockfile (go.sum, uv.lock/poetry.lock, Cargo.lock, package-lock.json/pnpm-lock.yaml/yarn.lock, pubspec.lock, gradle.lockfile) is tracked.

## Why

Builds are reproducible only when dependency versions are pinned in git.

## Check

For each `item` in:

```cel
repo.stacks
```

```cel
(item != "go" || "go.sum" in tracked() || !text("go.mod").contains("require")) && (item != "python" || tracked().exists(f, f in ["uv.lock", "poetry.lock", "pdm.lock", "Pipfile.lock", "requirements.lock", "requirements.txt"])) && (item != "rust" || "Cargo.lock" in tracked() || !text("Cargo.toml").matches("\\[(workspace\\.)?dependencies\\]")) && (item != "node" || tracked().exists(f, f in ["package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb", "bun.lock"])) && (item != "flutter" || "pubspec.lock" in tracked()) && (item != "kotlin" || tracked().exists(f, f in ["gradle.lockfile", "gradle/verification-metadata.xml"]))
```

## Fix

Generate the lockfile with the package manager and commit it; remove it from .gitignore.

Agent instruction: Run the package manager to produce the lockfile, remove any .gitignore entry for it, and commit it.

See: docs/baseline.md#dependencies
