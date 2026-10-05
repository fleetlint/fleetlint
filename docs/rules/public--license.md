# public/license

Public repository has a LICENSE

| | |
|---|---|
| Severity | error |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

A public repository has a LICENSE file (and an SPDX identifier in the package manifest).

## Why

Without a license, nobody may legally use the code.

## Applies when

```cel
repo.public
```

## Check

```cel
exists("LICENSE*") || exists("COPYING*")
```

## Fix

Add LICENSE (MIT or Apache-2.0) with the current year and your name.

Agent instruction: Ask the user which license; add the LICENSE file and the SPDX identifier to the package manifest.
