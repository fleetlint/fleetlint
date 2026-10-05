# slop/emoji-in-code

No emoji in source code

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

Source code, log messages and comments contain no emoji or decorative symbols.

## Why

They render inconsistently in terminals and logs and carry no information a word would not.

## Check

For each `item` in:

```cel
codegrep("code", r'[\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}\x{2B50}]')
```

```cel
false
```

## Fix

Replace the symbol with a word or remove it.

Agent instruction: Replace each symbol with plain text; keep symbols that are test data.

See: docs/baseline.md#slop
