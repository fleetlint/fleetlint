# repo/no-tracked-junk

No editor, OS or backup files are tracked

| | |
|---|---|
| Severity | error |
| Tiers | all |
| Stacks | any |
| Scope | each |
| Kind | expr |
| Source | fleetlint-minimal@0.1.0 |

## Requirement

Files such as .DS_Store, Thumbs.db, *.bak, *.orig, *.swp, *.log, nohup.out, Python bytecode, dependency directories and personal IDE state are never committed.

## Why

They are noise at best and leak local paths or data at worst.

## Check

For each `item` in:

```cel
tracked().filter(p, p.matches(r'(^|/)(\.DS_Store|Thumbs\.db|desktop\.ini|nohup\.out)$|\.(bak|orig|rej|swp|swo|log|pyc)$|~$|(^|/)(__pycache__|node_modules|\.venv|venv)/|(^|/)\.idea/workspace\.xml$|(^|/)\.vscode/settings\.json$'))
```

```cel
false
```

## Fix

git rm --cached the listed files and add their patterns to .gitignore.

Agent instruction: Untrack the listed junk files with git rm --cached, add matching patterns to .gitignore, commit as chore.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/baseline.md#hygiene
