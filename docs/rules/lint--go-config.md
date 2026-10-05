# lint/go-config

golangci-lint is configured strictly

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | go |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

A golangci-lint v2 config enables errcheck, govet, staticcheck, errorlint, gosec, gocognit and funlen at least.

## Why

Default linting misses the swallowed errors and oversized functions the baseline bans.

## Check

```cel
(file(".golangci.yml") || file(".golangci.yaml")) && (yaml(file(".golangci.yml") ? ".golangci.yml" : ".golangci.yaml").version == "2"
    || yaml(file(".golangci.yml") ? ".golangci.yml" : ".golangci.yaml").version == 2)
```

## Fix

Add .golangci.yml (version 2) from the Go stack profile.

Agent instruction: Create .golangci.yml with version: "2" and the linters listed in the Go stack profile, with a new-from-rev ratchet.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/stacks.md#go
