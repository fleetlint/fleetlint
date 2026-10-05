# containers/non-root-user

Containers do not run as root

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Every Dockerfile sets a non-root `USER`, or builds on a base image that is non-root by default (`:nonroot` tags).

## Why

A process that is root in the container is one kernel or runtime bug away from root on the host, and can rewrite everything in its own image.

## Applies when

```cel
tracked().exists(p, p.matches("(^|/)(Dockerfile|Containerfile)[^/]*$"))
```

## Check

For each `item` in:

```cel
tracked().filter(p, p.matches("(^|/)(Dockerfile|Containerfile)[^/]*$") && !p.endsWith(".dockerignore"))
```

```cel
lines(item).exists(l, l.matches("(?i)^\\s*USER\\s+\\S+") && !l.matches("(?i)^\\s*USER\\s+(root|0)(:\\S+)?\\s*$")) || lines(item).exists(l, l.matches("(?i)^\\s*FROM\\s+\\S*(:nonroot|-nonroot|cgr\\.dev/)"))
```

## Fix

Create a user in the final stage and add `USER <name>` before the entrypoint.

Agent instruction: In the final stage add a dedicated user and a USER instruction before ENTRYPOINT/CMD; make sure files the process writes are owned by that user.

See: docs/baseline.md#robustness
