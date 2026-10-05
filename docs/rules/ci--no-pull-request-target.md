# ci/no-pull-request-target

pull_request_target never runs untrusted code

| | |
|---|---|
| Severity | error |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

A `pull_request_target` workflow runs with repository secrets and must never check out or execute the PR's code.

## Why

This is the canonical workflow injection, used in real supply-chain attacks.

## Applies when

```cel
repo.public && workflows().exists(w, "pull_request_target" in triggers(w))
```

## Check

For each `item` in:

```cel
workflows().filter(w, "pull_request_target" in triggers(w))
```

```cel
!steps(item).exists(s, s.uses.startsWith("actions/checkout")
  && has(s.with.ref) && string(s.with.ref).matches("head|pull_request"))
```

## Fix

Use `pull_request` for anything that builds the PR; keep pull_request_target for labelling only.

Agent instruction: Change the trigger to pull_request, or remove the checkout of the PR head from the pull_request_target workflow.

See: docs/baseline.md#security
