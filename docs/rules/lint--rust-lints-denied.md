# lint/rust-lints-denied

clippy reports unwraps, panics and debug macros

| | |
|---|---|
| Severity | warning |
| Tiers | all |
| Stacks | rust |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

The `[lints.clippy]` table (or `[workspace.lints.clippy]`) sets every lint in `params.required` to warn or deny.

## Why

These lints are off by default, and they are the ones that find the crashes a Rust program can still have.

## Applies when

```cel
(has(toml("Cargo.toml").lints) && has(toml("Cargo.toml").lints.clippy)) || (has(toml("Cargo.toml").workspace) && has(toml("Cargo.toml").workspace.lints) && has(toml("Cargo.toml").workspace.lints.clippy))
```

## Check

For each `item` in:

```cel
params.required
```

```cel
(has(toml("Cargo.toml").lints) && has(toml("Cargo.toml").lints.clippy)
  && item in toml("Cargo.toml").lints.clippy && toml("Cargo.toml").lints.clippy[item] != "allow")
|| (has(toml("Cargo.toml").workspace) && has(toml("Cargo.toml").workspace.lints) && has(toml("Cargo.toml").workspace.lints.clippy)
  && item in toml("Cargo.toml").workspace.lints.clippy && toml("Cargo.toml").workspace.lints.clippy[item] != "allow")
```

## Parameters

- `required`: `[unwrap_used expect_used panic todo dbg_macro]`

## Fix

Add the missing lints to [lints.clippy] with level deny.

Agent instruction: Add each reported lint to [lints.clippy] as "deny"; where an unwrap is a proven invariant, replace it with expect and an INVARIANT comment instead of allowing the lint.

See: docs/stacks.md#rust
