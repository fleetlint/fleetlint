# ci/job-timeouts

Workflow jobs have timeouts

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Every job sets `timeout-minutes` (reusable-workflow jobs excepted) so a hang cannot burn the runner budget.

## Why

The default job timeout is six hours.

## Applies when

```cel
workflows().size() > 0
```

## Check

For each `item` in:

```cel
workflows()
```

```cel
jobs(item).all(j, j.timeout_minutes > 0 || j.uses != "")
```

## Fix

Add `timeout-minutes: 15` (or what the job needs) to each job.

Agent instruction: Add timeout-minutes to each job in the flagged workflow, sized to roughly twice its typical duration.
