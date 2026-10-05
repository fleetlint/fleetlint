# lint/go-linters-enabled

golangci-lint enables the baseline linters

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | go |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

The golangci-lint configuration enables every linter in `params.required`, directly or through `default: all`.

## Why

Swallowed errors, unsafe calls and oversized functions are exactly what these linters catch.

## Applies when

```cel
file(".golangci.yml") || file(".golangci.yaml")
```

## Check

For each `item` in:

```cel
params.required
```

```cel
yaml(file(".golangci.yml") ? ".golangci.yml" : ".golangci.yaml") != null && has(yaml(file(".golangci.yml") ? ".golangci.yml" : ".golangci.yaml").linters) && (
  (has(yaml(file(".golangci.yml") ? ".golangci.yml" : ".golangci.yaml").linters.default)
    && yaml(file(".golangci.yml") ? ".golangci.yml" : ".golangci.yaml").linters.default == "all")
  || (has(yaml(file(".golangci.yml") ? ".golangci.yml" : ".golangci.yaml").linters.default)
    && yaml(file(".golangci.yml") ? ".golangci.yml" : ".golangci.yaml").linters.default == "standard"
    && item in ["errcheck", "govet", "staticcheck", "unused", "ineffassign"])
  || (has(yaml(file(".golangci.yml") ? ".golangci.yml" : ".golangci.yaml").linters.enable)
    && item in yaml(file(".golangci.yml") ? ".golangci.yml" : ".golangci.yaml").linters.enable)
)
```

## Parameters

- `required`: `[errcheck govet staticcheck errorlint gosec gocognit funlen]`

## Fix

Add the missing linters to linters.enable in .golangci.yml.

Agent instruction: Add each reported linter to the linters.enable list in .golangci.yml; keep the rest of the file unchanged.

See: docs/stacks.md#go
