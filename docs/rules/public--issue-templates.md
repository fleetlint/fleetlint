# public/issue-templates

Public repository has issue or pull-request templates

| | |
|---|---|
| Severity | info |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

An issue template or a pull-request template exists in `.github/` or `.gitea/`.

## Why

A template asks for the version, the steps and the expected result once, instead of in the first three comments of every report.

## Applies when

```cel
repo.public
```

## Check

```cel
exists(".github/ISSUE_TEMPLATE/*") || exists(".gitea/ISSUE_TEMPLATE/*") || exists(".gitea/issue_template/*") || exists(".github/ISSUE_TEMPLATE.md") || exists(".github/issue_template.md") || exists(".gitea/issue_template.md") || exists(".github/PULL_REQUEST_TEMPLATE*") || exists(".github/pull_request_template*") || exists(".gitea/PULL_REQUEST_TEMPLATE*") || exists(".gitea/pull_request_template*")
```

## Fix

Add .github/ISSUE_TEMPLATE/bug.md asking for version, steps and expected result; add a pull-request template listing what `make check` must show.

Agent instruction: Add a bug-report issue template and a pull-request template under .github/ (Gitea reads them too); keep each under 20 lines.

See: docs/baseline.md#public-repositories
