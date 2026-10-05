# lint/rust-config

clippy lints are configured in Cargo.toml

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | rust |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Cargo.toml configures clippy (pedantic warn, unwrap_used/expect_used/panic deny outside tests) in a [lints] table.

## Why

Unwrap-driven crashes are the Rust equivalent of swallowed errors.

## Check

```cel
has(toml("Cargo.toml").lints) || has(toml("Cargo.toml").workspace.lints)
```

## Fix

Add the [lints] table from the Rust stack profile.

Agent instruction: Add [lints.rust] and [lints.clippy] tables per the Rust profile to Cargo.toml.
