# 0003 — CEL for rule predicates

Date: 2026-10-04. Status: accepted.

## Context
Predicates need boolean logic, comparisons, quantifiers over lists and regex matching, with guaranteed termination and no side effects, in a syntax people may already know.

## Decision
Google's Common Expression Language via `cel-go`. Expressions are compiled and type-checked when the config loads, so a typo is a config error, not a silently passing rule. Results must be bool. Accessors (`exists`, `file`, `glob`, `tracked`, `text`, `lines`, `yaml`, `toml`, `json`, `makefile`, `scan`, `matchesAny`) are the whole surface.

## Consequences
`has` is a CEL macro, so the exact-path accessor is named `file`. A failing expression says only that it was false; rules carry a `message`, and list checks that need to name the failing element are written in Go.
