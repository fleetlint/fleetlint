# taskrunner/targets

Task runner exposes the contract targets

| | |
|---|---|
| Severity | error |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

The task runner exposes fmt, lint, test, cover, audit, check and check-fast (`params.required`), so every tool and person can run the same commands in every repository. Any runner counts: a Makefile, a justfile, a Taskfile or package.json scripts; with Gradle the built-in `check` task is accepted. `facts.taskrunner` names the runner when a repository has more than one.

## Why

Hooks, CI and automation all depend on `check` meaning the same thing everywhere.

## Applies when

```cel
taskrunner.kind != "gradle"
```

## Check

For each `item` in:

```cel
params.required
```

```cel
item in taskrunner.targets && !(item in taskrunner.recipes && taskrunner.recipes[item].size() > 0
     && taskrunner.recipes[item].all(l, l.matches("(?i)echo\\s+\"?todo")))
```

## Parameters

- `required`: `[fmt lint test cover audit check check-fast]`

## Fix

Add the missing targets; `fleetlint fix --apply` writes a template for the runner (a Makefile unless `facts.taskrunner` says just or task) when there is none.

Agent instruction: Add the missing targets to the task runner following its template. Fill each with the stack's real command, no TODO placeholders in check-fast or check.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/baseline.md#task-runner-contract
