# repo/gitattributes

.gitattributes normalizes line endings

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

.gitattributes sets `* text=auto eol=lf` and marks binaries.

## Why

Prevents CRLF/LF diffs and corrupted binaries across operating systems.

## Check

```cel
file(".gitattributes") && text(".gitattributes").contains("text=auto")
```

## Fix

Add .gitattributes with `* text=auto eol=lf`; `fleetlint fix --apply` writes the template.

Agent instruction: Create .gitattributes with `* text=auto eol=lf` and `binary` for image, font, archive and media extensions.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).
