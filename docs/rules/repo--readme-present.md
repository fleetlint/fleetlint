# repo/readme-present

README exists and has a quickstart

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | any |
| Scope | root |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

README.md says what the project is and how to install, run and test it.

## Why

The first screen of the README must be enough to run the project.

## Check

```cel
exists("README.md") && size(lines("README.md")) >= 10 && text("README.md").matches('(?i)(## |### )?(install|quick ?start|getting started|usage|setup)')
```

## Fix

Write a README with purpose, requirements, quickstart and the task-runner targets.

Agent instruction: Write README.md: 2-3 sentence purpose, requirements, quickstart commands, the make targets, configuration table. Plain language, no marketing.

See: docs/baseline.md#documentation
