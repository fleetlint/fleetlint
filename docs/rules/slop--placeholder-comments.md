# slop/placeholder-comments

No placeholder implementations

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

No code is marked as a stand-in ("in a real app", "for simplicity", "not implemented", "mock data for now").

## Why

The comment admits the code does not do what its name says.

## Check

For each `item` in:

```cel
codegrep("code", r'(?i)(//|#|/\*)\s*.*\b(in a real (app|implementation|world|project|scenario)|for simplicity|simplified (version|implementation|logic)|placeholder (implementation|logic|for now)|mock (data|implementation) for now|not (yet )?implemented|implement (this|later|me)|dummy (data|value|implementation)|you (would|should|could|might) (want|need) to)')
```

```cel
false
```

## Fix

Implement it, or remove the code and track the gap in an issue.

Agent instruction: Report each placeholder; implement it only when the intended behaviour is clear from the callers, otherwise ask.

See: docs/baseline.md#slop
