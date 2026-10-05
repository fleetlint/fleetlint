# repo/editorconfig

.editorconfig is present

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

An .editorconfig declares charset, line endings, final newline and indentation.

## Why

It stops formatting churn between editors before any formatter runs.

## Check

```cel
file(".editorconfig")
```

## Fix

Add .editorconfig with `root = true`; `fleetlint fix --apply` writes the template.

Agent instruction: Create .editorconfig: utf-8, lf, insert_final_newline, trim_trailing_whitespace, 2-space default with 4 for Go/Kotlin/Python/Rust/Dart.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).
