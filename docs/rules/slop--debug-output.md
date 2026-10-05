# slop/debug-output

No debug prints outside tests

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

Non-test code writes through the logger or the program's output layer, not through raw print calls.

## Why

Raw prints cannot be levelled, redacted or switched off, and they leak into the output of anything that embeds the code.

## Check

For each `item` in:

```cel
codegrep("nontest", r'(^|[^.\w])print\(|\bconsole\.(log|debug|info)\(|(^|[^.\w])println!?\(|\bfmt\.Print|\bdebugPrint\(|\bdbg!\(')
```

```cel
false
```

## Fix

Use the logger; for a CLI, the output stream the command was given.

Agent instruction: Replace with the project's logger or output writer; delete prints that are debugging leftovers.

See: docs/baseline.md#slop
