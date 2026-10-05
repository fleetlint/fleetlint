# repo/codeowners

CODEOWNERS names reviewers

| | |
|---|---|
| Severity | info |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

A CODEOWNERS file routes review requests for every path.

## Why

Review ownership that lives in people's heads does not survive a holiday.

## Check

```cel
file("CODEOWNERS") || file(".github/CODEOWNERS") || file("docs/CODEOWNERS") || file(".gitea/CODEOWNERS")
```

## Fix

Add .github/CODEOWNERS with a default owner line.

Agent instruction: Add .github/CODEOWNERS containing `* @<owner>` and commit.
