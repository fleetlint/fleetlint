# lint/kotlin-config

detekt is configured

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | kotlin |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

detekt runs with a config enforcing EmptyCatchBlock, SwallowedException, LongMethod and complexity limits.

## Why

Kotlin's compiler accepts swallowed exceptions; detekt is where they are caught.

## Check

```cel
exists("**/detekt*.yml") || exists("**/detekt*.yaml") || exists("config/detekt/*")
```

## Fix

Add config/detekt/detekt.yml and the detekt Gradle plugin.

Agent instruction: Add the detekt Gradle plugin with buildUponDefaultConfig and a config/detekt/detekt.yml enabling the rules from the Kotlin stack profile.
