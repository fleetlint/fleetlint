# slop/emoji-headings

No emoji in documentation headings

| | |
|---|---|
| Severity | info |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-slop@0.1.0 |

## Requirement

Headings in Markdown documentation contain no emoji.

## Why

They break anchors and search and mark the text as unedited generator output.

## Check

For each `item` in:

```cel
grep("**/*.md", r'^#{1,6}\s.*[\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}]')
```

```cel
item.path.endsWith("CHANGELOG.md")
```

## Fix

Remove the emoji from the heading.

Agent instruction: Remove emoji from headings and update links to the changed anchors.

See: docs/baseline.md#slop
