# slop/skipped-tests

No skipped tests are committed

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

Tests are not skipped; a quarantined flaky test references an issue.

## Why

A skipped test is coverage that the numbers still count and the suite no longer provides.

## Check

For each `item` in:

```cel
codegrep("test", r'\.skip\(|\bxit\(|\bxdescribe\(|@Ignore\b|@Disabled\b|t\.Skip\(|#\[ignore\]|@pytest\.mark\.skip|@unittest\.skip|\bskip:\s*(true|[\x22\x27])')
```

```cel
false
```

## Fix

Fix or delete the test; if it must be quarantined, reference the issue.

Agent instruction: Report each skipped test with its reason; do not un-skip tests that then fail without fixing the cause.

See: docs/baseline.md#slop
