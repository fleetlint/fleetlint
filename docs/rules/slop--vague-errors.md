# slop/vague-errors

Error messages say what failed

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

No error message is just "something went wrong", "an error occurred" or "unknown error".

## Why

The person reading it cannot act on it and the person debugging it cannot find it.

## Check

For each `item` in:

```cel
codegrep("code", r'(?i)\x22(something went wrong|an error (has )?occurred|unknown error|oops)\x22')
```

```cel
false
```

## Fix

Say what failed and with which input.

Agent instruction: Rewrite the message to name the operation and the offending input; do not include secrets.

See: docs/baseline.md#slop
