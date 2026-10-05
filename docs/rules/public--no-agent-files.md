# public/no-agent-files

Public repositories track no agent instruction files

| | |
|---|---|
| Severity | error |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-minimal@0.1.0 |

## Requirement

CLAUDE.md, AGENTS.md, .claude/ and .agent/ stay untracked in public repositories (listed in .git/info/exclude).

## Why

The owner's policy is that public repositories carry no AI tooling artefacts.

## Applies when

```cel
repo.public
```

## Check

```cel
!tracked().exists(f, f.matches('(^|/)(CLAUDE|CLAUDE\\.local|AGENTS)\\.md$')
                  || f.startsWith('.claude/') || f.startsWith('.agent/'))
```

## Fix

git rm --cached the files and list them in .git/info/exclude so they stay local.

Agent instruction: Untrack the listed files with git rm --cached, add them to .git/info/exclude (not .gitignore), and commit.

See: docs/baseline.md#public-repositories
