# deps/install-scripts-disabled

Package install scripts are disabled

| | |
|---|---|
| Severity | info |
| Tiers | 1, 2 |
| Stacks | node |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Install scripts of dependencies do not run by default: `ignore-scripts=true` in .npmrc, `enableScripts: false` in .yarnrc.yml, or an explicit allowlist (`onlyBuiltDependencies` for pnpm, `trustedDependencies` for bun).

## Why

Install scripts are the usual way a compromised package executes code on developer machines and in CI.

## Check

```cel
scan(".npmrc", "^\\s*ignore-scripts\\s*=\\s*true").size() > 0 || scan(".yarnrc.yml", "^\\s*enableScripts:\\s*false").size() > 0 || text("pnpm-workspace.yaml").contains("onlyBuiltDependencies") || text("package.json").contains("onlyBuiltDependencies") || text("package.json").contains("trustedDependencies")
```

## Fix

Add `ignore-scripts=true` to .npmrc and allow the few packages that need a build step explicitly.

Agent instruction: Add ignore-scripts=true to .npmrc (or the package manager's equivalent), run a clean install and the tests, and list the packages that need their build script allowed.

See: docs/baseline.md#dependencies
