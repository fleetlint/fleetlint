# public/code-of-conduct

Public repository has a code of conduct

| | |
|---|---|
| Severity | info |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

A code of conduct (Contributor Covenant is fine) with a contact address.

## Why

Forges surface it in the community profile; contributors look for it before engaging.

## Applies when

```cel
repo.public
```

## Check

```cel
exists("CODE_OF_CONDUCT*") || exists(".github/CODE_OF_CONDUCT*")
```

## Fix

Add CODE_OF_CONDUCT.md (Contributor Covenant 2.1) with a contact address.

Agent instruction: Add CODE_OF_CONDUCT.md using the Contributor Covenant 2.1 text; ask the user for the contact address.
