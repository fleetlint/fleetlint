# 0002 — Rules are code, configuration is data

Date: 2026-10-04. Status: accepted.

## Context
Repository linters tend to grow a YAML condition language with one rule kind per need (alint has 94). Those languages reach 60% of cases and then fight every interesting check, such as "the release pipeline is consistent".

## Decision
Two kinds of rule. `expr`: a CEL predicate over a repo model, declared in catalogs; covers presence, config values and consistency between files. `go`: a registered implementation for anything needing real logic, grading or external tools. Catalogs hold the metadata (severity, tiers, requirement, fix) for both, so documentation is generated from one place. `command`: a local executable as the escape hatch, only from the repo's own config.

## Consequences
Adding a check is usually a YAML entry; adding a rule family is a Go file with fixtures. The repo model exposed to CEL is the design surface to get right; it is documented from `internal/celenv`.
