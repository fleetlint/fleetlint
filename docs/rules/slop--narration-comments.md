# slop/narration-comments

Comments do not narrate the change

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

Comments say why the code is the way it is, not what was changed or asked for ("Now uses…", "Fixed:", "As requested").

## Why

The history records what changed; a comment that repeats it is wrong after the next edit.

## Check

For each `item` in:

```cel
codegrep("code", r'(//|#|/\*)\s*(Now (we|it|uses|handles|correctly|properly)\b|Updated? to\b|Changed (to|from)\b|Fix(ed)? for\b|(NEW|FIX|FIXED|CHANGED|UPDATED):|New:|Fix:|Previously\b|As requested\b|Per (your|the) request\b|This is the (key|main|critical|important) (fix|change|part)\b)')
```

```cel
false
```

## Fix

Delete the comment, or rewrite it to state the constraint the code satisfies.

Agent instruction: Delete narration comments; keep a comment only if it explains a constraint, and rewrite it to say that.

See: docs/baseline.md#slop
