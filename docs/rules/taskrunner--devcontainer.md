# taskrunner/devcontainer

The task runner uses the dev container

| | |
|---|---|
| Severity | warning |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

When the dev container flag is set (a devcontainer.json exists, or `facts.devcontainer: true`), `make <target>` called outside the container runs the target inside it, so results do not depend on what is installed on the machine. A switch (`DEVCONTAINER=0`) keeps running on the machine possible. Repositories that do not use a dev container run on the machine and are not checked; `facts.devcontainer: false` says so explicitly.

## Why

A dev container that the gates do not use is a second environment next to the real one, and the two drift apart.

## Applies when

```cel
repo.devcontainer && taskrunner.kind == "make"
```

## Check

```cel
text("Makefile").matches("devcontainer (exec|up)")
```

## Fix

Forward the targets into the container when make runs outside it (`fleetlint explain taskrunner/devcontainer` shows the switch), or set `facts.devcontainer: false`.

Agent instruction: At the top of the Makefile add `DEVCONTAINER ?= 1` and, when `$(DEVCONTAINER)$(IN_DEVCONTAINER)` equals 1, define every target as `devcontainer up --workspace-folder . && devcontainer exec --workspace-folder . make $@ DEVCONTAINER=0`; keep the existing recipes in the else branch. Set containerEnv IN_DEVCONTAINER=1 in devcontainer.json.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/baseline.md#task-runner-contract
