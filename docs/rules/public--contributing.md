# public/contributing

Public repository explains how to contribute

| | |
|---|---|
| Severity | info |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

CONTRIBUTING.md states how to set up, run `make check`, and what a pull request needs.

## Why

It is the difference between a drive-by issue and a merged fix.

## Applies when

```cel
repo.public
```

## Check

```cel
exists("CONTRIBUTING*") || exists(".github/CONTRIBUTING*") || exists("docs/CONTRIBUTING*")
```

## Fix

Add CONTRIBUTING.md: setup, make check, commit convention, PR expectations.

Agent instruction: Write CONTRIBUTING.md from the Makefile targets and hook configuration actually present; keep it under one page.
