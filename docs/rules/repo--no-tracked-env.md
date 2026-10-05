# repo/no-tracked-env

No .env files are tracked

| | |
|---|---|
| Severity | error |
| Tiers | all |
| Stacks | any |
| Scope | each |
| Kind | expr |
| Source | fleetlint-minimal@0.1.0 |

## Requirement

Environment files that may hold credentials are never committed; templates with placeholder values are fine.

## Why

A tracked .env is the most common way credentials leak into history.

## Check

```cel
!tracked().exists(f, f.matches('(^|/)\\.env(\\.[^/]*)?$')
                  && !f.matches('\\.(example|sample|template|dist)$'))
```

## Fix

git rm --cached the .env file, add .env* to .gitignore, rotate any secret it contained.

Agent instruction: Untrack the .env file, add `.env` and `.env.*` (but not `.env.example`) to .gitignore, and tell the user which secrets must be rotated.

See: docs/baseline.md#security
