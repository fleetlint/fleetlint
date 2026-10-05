# Baseline and nested configuration

## Baseline: adopt now, hold new code to the rules

`fleetlint baseline` writes `.fleetlint-baseline.json` with a fingerprint (rule, scope, path, message) of every current finding. From then on those findings report as *baselined* instead of failing, so `fleetlint check` is green today and fails only on findings that are new.

The file can only shrink. Re-running `fleetlint baseline` drops entries that no longer match and refuses to add new ones; the report says how many entries are "paid off" after each run. `--reset` starts over and grandfathers everything, which is a deliberate act you should see in a diff. Commit the file; a pull request that grows it is a pull request that weakens the policy.

Baselined findings do not count towards compliance in fleet mode: they are acknowledged debt, listed separately, not success.

A baseline and an exception are different tools: an exception is a reasoned, visible acceptance of a specific finding, optionally with a deadline; a baseline is unexplained legacy that is expected to go away.

## Nested configuration in monorepos

A workspace member may carry its own `.fleetlint.yaml`:

```yaml
version: 1
facts: { tier: 2 }
rules:
  lint/go-config: { enabled: false, reason: "uses the root .golangci.yml" }
  taskrunner/targets: { severity: warning }
exceptions:
  - rule: repo/no-tracked-junk
    match: "testdata/*.log"
    reason: "parser fixtures"
```

It applies to that scope only and may override facts, configure rules (disable with a reason, change severity, set params) and add exceptions. It cannot extend catalogs or define new rules; those belong in the root file, so there is one place where policy is resolved. Nested exceptions are reported alongside the root's and appear in the fleet policy notes.
