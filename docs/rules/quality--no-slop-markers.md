# quality/no-slop-markers

Documentation is free of marketing filler

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

READMEs and docs describe what the software does and how to use it, without the adjectives from the slop catalog.

## Why

Filler words are the most recognisable trace of generated prose and say nothing a reader can act on.

## Check

For each `item` in:

```cel
grep("**/*.md", "\\b(seamless(ly)?|blazing(ly)?[- ]fast|robust|powerful|comprehensive|cutting[- ]edge|state[- ]of[- ]the[- ]art|leverag(e|es|ing)|effortless(ly)?|supercharge|game[- ]chang(er|ing)|world[- ]class|delve|elevate)\\b")
```

```cel
item.path.endsWith("CHANGELOG.md")
```

## Fix

Rewrite the line to state the concrete behaviour; delete the adjective.

Agent instruction: Rewrite each flagged line without the flagged word, keeping the technical content; do not add new adjectives.

See: docs/baseline.md#documentation
