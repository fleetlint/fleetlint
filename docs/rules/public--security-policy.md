# public/security-policy

Public repository has SECURITY.md

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

SECURITY.md explains how to report a vulnerability privately.

## Why

Reporters otherwise open public issues, which disclose the problem before a fix exists.

## Applies when

```cel
repo.public && repo.tier == 1
```

## Check

```cel
file("SECURITY.md") || file(".github/SECURITY.md")
```

## Fix

Add SECURITY.md with a private contact and expected response time.

Agent instruction: Add SECURITY.md: supported versions, private reporting channel, response expectation.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).
