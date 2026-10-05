# slop/variant-files

No files named as copies or variants

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

No file is named as a variant of another (`client_v2.py`, `handler-old.ts`, `config copy.json`).

## Why

The history keeps old versions; a second file means nobody decided which one is current.

## Check

For each `item` in:

```cel
tracked().filter(p, p.matches(r'(_|-)(v[2-9]|old|new|backup|copy|fixed|improved|enhanced)\.[a-z]+$|( copy|\([0-9]+\))\.[a-z]+$'))
```

```cel
false
```

## Fix

Keep one file under the original name and delete the other.

Agent instruction: Report the pair; do not delete either without confirming which is in use.

See: docs/baseline.md#slop
