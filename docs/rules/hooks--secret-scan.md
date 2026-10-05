# hooks/secret-scan

Hooks scan for secrets

| | |
|---|---|
| Severity | error |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-minimal@0.1.0 |

## Requirement

A pre-commit hook blocks commits containing secrets.

## Why

Secrets must be stopped before they enter history; removing them afterwards means rotation and rewrites.

## Applies when

```cel
file(".pre-commit-config.yaml")
```

## Check

```cel
yaml(".pre-commit-config.yaml").repos.exists(r,
  r.repo.contains("gitleaks") ||
  (has(r.hooks) && r.hooks.exists(h, h.id in ["gitleaks", "detect-private-key", "detect-secrets"])))
```

## Fix

Add the gitleaks hook (repo: https://github.com/gitleaks/gitleaks) to .pre-commit-config.yaml.

Agent instruction: Add the gitleaks pre-commit hook to .pre-commit-config.yaml and run `prek update`.
