# lint/node-config

TypeScript strict mode and a linter are configured

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | node |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

tsconfig.json has strict: true (plus noUncheckedIndexedAccess) and ESLint strictTypeChecked or Biome is configured.

## Why

Without strict types and a type-aware linter, floating promises and any-typed code go unnoticed.

## Check

```cel
(file("tsconfig.json") && json("tsconfig.json").compilerOptions.strict == true) && (exists("eslint.config.*") || file("biome.json") || file("biome.jsonc"))
```

## Fix

Enable strict in tsconfig.json and add eslint.config.js per the Node stack profile.

Agent instruction: Set compilerOptions.strict and noUncheckedIndexedAccess in tsconfig.json; add eslint.config.js with typescript-eslint strictTypeChecked.
