# slop/report-documents

No generated report documents are tracked

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

No status or summary documents such as `FIX_SUMMARY.md`, `IMPLEMENTATION_REPORT.md` or `NEXT_STEPS.md` are tracked.

## Why

They describe one working session, go stale with the next commit and bury the documentation that is maintained.

## Check

For each `item` in:

```cel
tracked().filter(p, p.matches(r'^(docs/)?[A-Z0-9_-]*(SUMMARY|REPORT|FIXES|IMPLEMENTATION|PROGRESS|ANALYSIS|REFACTOR(ING)?_PLAN|CHANGES_MADE|UPDATES|NEXT_STEPS)[A-Z0-9_-]*\.md$'))
```

```cel
false
```

## Fix

Delete the file; move anything lasting into the README, an ADR or the changelog.

Agent instruction: Move lasting content into README, docs/adr or CHANGELOG and delete the file.

See: docs/baseline.md#slop
