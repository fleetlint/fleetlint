# repo/no-tracked-junk

No editor, OS or backup files are tracked

| | |
|---|---|
| Severity | error |
| Tiers | all |
| Stacks | any |
| Scope | each |
| Kind | go |
| Source | fleetlint-minimal@0.1.0 |

## Requirement

Files such as .DS_Store, Thumbs.db, *.bak, *.orig, *.swp, *.log and nohup.out are never committed.

## Why

They are noise at best and leak local paths or data at worst.

## Fix

git rm --cached the listed files and add their patterns to .gitignore.

Agent instruction: Untrack the listed junk files with git rm --cached, add matching patterns to .gitignore, commit as chore.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/baseline.md#hygiene
