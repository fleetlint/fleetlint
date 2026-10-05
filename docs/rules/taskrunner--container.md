# taskrunner/container

The task runner uses the container

| | |
|---|---|
| Severity | warning |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | outcome |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

When `facts.container` is devcontainer (the default once a devcontainer.json exists), docker or podman, a target called outside the container runs inside it, so results do not depend on what is installed on the machine. Any way of getting there counts: the devcontainer CLI, `docker run` or `docker compose exec`, podman; another one is added with `accept:`. A switch (`CONTAINER=0`) keeps running on the machine possible, and `facts.container: none` says the repository runs there.

## Why

A container that the gates do not use is a second environment next to the real one, and the two drift apart.

## Applies when

```cel
repo.container != "none" && taskrunner.kind in ["make", "just", "task"]
```

## Fix

Forward the targets into the container when the runner is called outside it (`fleetlint fix` writes such a runner file when there is none), or set `facts.container: none`.

Agent instruction: At the top of the task runner file add a CONTAINER switch defaulting to 1 and, when it is 1 and IN_CONTAINER is unset, run each target inside the container (devcontainer exec, docker run or podman run); keep the existing recipes for the other case. Set IN_CONTAINER=1 inside the container.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/baseline.md#task-runner-contract
