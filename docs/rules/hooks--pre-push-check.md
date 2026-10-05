# hooks/pre-push-check

Full check runs before push

| | |
|---|---|
| Severity | warning |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

The task runner's `check` (`make check`, `just check`, `task check`, `npm run check`, `./gradlew check`) runs as a pre-push hook.

## Why

Pushing red is what breaks main for everyone else; the hook makes it impossible to do by accident.

## Applies when

```cel
file(".pre-commit-config.yaml")
```

## Check

```cel
yaml(".pre-commit-config.yaml").repos.exists(r, has(r.hooks) && r.hooks.exists(h,
  has(h.stages) && "pre-push" in h.stages && has(h.entry) && h.entry.contains("check")))
```

## Fix

Add a local hook with entry `make check`, stages: [pre-push], always_run: true.

Agent instruction: Add a `repo: local` hook running `make check` at stage pre-push with pass_filenames: false and always_run: true.
