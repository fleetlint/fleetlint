# lint/node-strict-flags

tsconfig enables the strict compiler flags

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | node |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

tsconfig.json sets every flag in `params.required` to true. A tsconfig that `extends` another configuration is not checked, because the flags may come from there.

## Why

`strict` alone leaves indexed access, optional properties and switch fallthrough unchecked; these flags close the gaps that produce undefined at run time.

## Applies when

```cel
file("tsconfig.json") && json("tsconfig.json") != null && !has(json("tsconfig.json").extends)
```

## Check

For each `item` in:

```cel
params.required
```

```cel
has(json("tsconfig.json").compilerOptions) && item in json("tsconfig.json").compilerOptions && json("tsconfig.json").compilerOptions[item] == true
```

## Parameters

- `required`: `[strict noUncheckedIndexedAccess exactOptionalPropertyTypes noImplicitOverride noFallthroughCasesInSwitch]`

## Fix

Set the missing flags to true under compilerOptions.

Agent instruction: Set each reported flag to true in tsconfig.json compilerOptions, run tsc --noEmit and list the errors that appear instead of silencing them.

See: docs/stacks.md#nodejs--typescript
