# docs/architecture

Architecture is documented

| | |
|---|---|
| Severity | warning |
| Tiers | 1 |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

docs/ARCHITECTURE.md (or an Architecture section in the README) explains the components, their boundaries and the data flow in one page.

## Why

The shape of the system is the one thing a newcomer cannot recover from the code quickly.

## Check

```cel
file("docs/ARCHITECTURE.md") || file("ARCHITECTURE.md") || (file("README.md") && scan("README.md", "(?i)^#+\\s*architecture").size() > 0)
```

## Fix

Write docs/ARCHITECTURE.md: components, boundaries, data flow, the three decisions that shaped it.

Agent instruction: Write docs/ARCHITECTURE.md from the actual package structure: one paragraph per component, one diagram, no marketing.

See: docs/baseline.md#documentation
