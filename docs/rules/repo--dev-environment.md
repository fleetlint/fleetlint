# repo/dev-environment

The development tools are reproducible

| | |
|---|---|
| Severity | warning |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | outcome |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Someone with a fresh machine gets the same toolchain and tools as everyone else from what is in the repository: a dev container (`.devcontainer/devcontainer.json`), a Nix flake or devenv, a version manager file (`mise.toml`, `.tool-versions`), or a `tools` target that installs pinned versions. Any one of them is enough; the task runner stays the interface that hooks and CI use. When the dev container flag is set (a devcontainer.json exists, or `facts.devcontainer: true`), the dev container is the mechanism and has to be there.

## Why

Lint and test results that depend on which linter version a contributor happens to have are not results; a second contributor's first hour should not go into guessing tools.

## Fix

Choose. For a dev container set `facts.devcontainer: true` and run `fleetlint fix --apply`: it writes the container for the repository's stacks, and the Makefile and check workflow it writes run inside it. To stay on the machine, give the `tools` target pinned versions or add a version-manager file.

Agent instruction: Ask which mechanism the user wants. Dev container: set facts.devcontainer true in .fleetlint.yaml and run `fleetlint fix --apply`. Machine: add a `tools` target to the task runner that installs every tool at a fixed version. Do not add more than one mechanism.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/baseline.md#task-runner-contract
