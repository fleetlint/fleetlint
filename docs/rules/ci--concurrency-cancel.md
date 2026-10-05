# ci/concurrency-cancel

PR workflows cancel superseded runs

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Workflows triggered by pull requests declare `concurrency: {group: ..., cancel-in-progress: true}`.

## Why

Every push to a PR otherwise queues another full run behind the obsolete one.

## Applies when

```cel
workflows().exists(w, "pull_request" in triggers(w))
```

## Check

For each `item` in:

```cel
workflows().filter(w, "pull_request" in triggers(w))
```

```cel
yaml(item) != null && has(yaml(item).concurrency) && yaml(item).concurrency["cancel-in-progress"] != false
```

## Fix

Add a concurrency block keyed on the workflow and ref with cancel-in-progress: true.

Agent instruction: Add `concurrency:\n  group: ${{ github.workflow }}-${{ github.ref }}\n  cancel-in-progress: true` at the top level of the workflow.
