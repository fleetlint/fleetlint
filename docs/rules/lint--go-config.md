# lint/go-config

A Go linter is configured strictly

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | go |
| Scope | each |
| Kind | outcome |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

A Go linter configuration is committed: golangci-lint v2 enabling errcheck, govet, staticcheck, errorlint, gosec, gocognit and funlen at least, or staticcheck together with revive. Another linter setup is added with `accept:`.

## Why

Default linting misses the swallowed errors and oversized functions the baseline bans.

## Fix

Add .golangci.yml (version 2) from the Go stack profile.

Agent instruction: Create .golangci.yml with version: "2" and the linters listed in the Go stack profile, with a new-from-rev ratchet.

`fleetlint fix` can apply this rule's fix automatically (1 action(s)).

See: docs/stacks.md#go
