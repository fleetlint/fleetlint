# docs/adr-directory

Decisions are recorded as ADRs

| | |
|---|---|
| Severity | info |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Significant decisions live in docs/adr/NNNN-title.md (context, decision, consequences).

## Why

Without the why, the next person re-litigates every settled question.

## Check

```cel
exists("docs/adr/*.md") || exists("docs/decisions/*.md") || exists("adr/*.md") || exists("docs/adrs/*.md")
```

## Fix

Add docs/adr/0001-record-architecture-decisions.md and one ADR per existing major decision.

Agent instruction: Create docs/adr/0001-record-architecture-decisions.md in MADR format; list the decisions already visible in the code as candidates.
