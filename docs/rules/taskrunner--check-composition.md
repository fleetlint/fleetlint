# taskrunner/check-composition

check runs the gates and excludes bench and release

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

`check` is the definition of green: lint, test, cover, audit and build. Benchmarks are noisy and release is a deliberate act, so neither is part of it.

## Why

A `check` that is slow or flaky gets bypassed, and a bypassed gate is worse than none.

## Applies when

```cel
taskrunner.kind in ["make", "just", "task"] && "check" in taskrunner.targets
```

## Check

```cel
"check" in taskrunner.deps && ["lint", "test"].all(t, t in taskrunner.deps["check"]) && !("bench" in taskrunner.deps["check"]) && !("release" in taskrunner.deps["check"])
```

## Fix

Make `check` depend on lint, test, cover, audit and build, and move bench/release out.

Agent instruction: Rewrite the check target's prerequisites to `lint test cover audit build`; keep bench and release as separate targets.
