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

`make check` is the definition of green: lint, test, cover, audit and build. Benchmarks are noisy and release is a deliberate act, so neither is part of it.

## Why

A `check` that is slow or flaky gets bypassed, and a bypassed gate is worse than none.

## Applies when

```cel
file("Makefile") && "check" in makefile("Makefile").targets
```

## Check

```cel
["lint", "test"].all(t, t in makefile("Makefile").deps["check"]) && !("bench" in makefile("Makefile").deps["check"]) && !("release" in makefile("Makefile").deps["check"])
```

## Fix

Set `check: lint test cover audit build` and move bench/release out.

Agent instruction: Rewrite the check target's prerequisites to `lint test cover audit build`; keep bench and release as separate targets.
