# slop/trivial-asserts

No assertions that cannot fail

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

No test asserts a constant (`expect(true).toBe(true)`, `assert True`).

## Why

It raises coverage and tests nothing.

## Check

For each `item` in:

```cel
codegrep("test", r'expect\((true|1)\)\.(toBe|toEqual)\((true|1)\)|assert\s+True\b|assertTrue\(true\)|assert!\(true\)|expect\(true,\s*(isTrue|true)\)')
```

```cel
false
```

## Fix

Assert the behaviour, or delete the test.

Agent instruction: Replace with an assertion on the observable outcome; delete the test if there is none.

See: docs/baseline.md#slop
