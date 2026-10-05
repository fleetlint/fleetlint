# containers/dockerignore

Container builds have a .dockerignore

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

A repository with a Dockerfile has a `.dockerignore` that keeps `.git`, secrets and build output out of the build context.

## Why

Without it the whole working tree, including `.env` files and the git history, is sent to the builder and can end up in a layer.

## Applies when

```cel
tracked().exists(p, p.matches("(^|/)(Dockerfile|Containerfile)[^/]*$"))
```

## Check

```cel
file(".dockerignore") || file(".containerignore") || exists("*.dockerignore")
```

## Fix

Add .dockerignore listing .git, .env*, build output and anything the image does not need.

Agent instruction: Add .dockerignore with .git, .env*, the stack's build output and dependency directories; check that the image still builds.

See: docs/baseline.md#robustness
