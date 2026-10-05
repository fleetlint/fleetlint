# ci/least-privilege

Workflows declare minimal permissions

| | |
|---|---|
| Severity | warning |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Every workflow sets top-level `permissions` (normally `contents: read`) and escalates per job only.

## Why

The default token has write access to the repo; a compromised action in a read-only workflow can do far less.

## Applies when

```cel
exists(".github/workflows/*.y*ml")
```

## Check

For each `item` in:

```cel
glob(".github/workflows/*.y*ml")
```

```cel
yaml(item) != null && has(yaml(item).permissions)
```

## Fix

Add `permissions: { contents: read }` at the top of each workflow and widen per job.

Agent instruction: Add top-level `permissions:\n  contents: read` to every workflow lacking it; move any wider permission to the job that needs it.

See: docs/baseline.md#security
