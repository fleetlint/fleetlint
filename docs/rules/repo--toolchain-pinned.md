# repo/toolchain-pinned

Toolchain version is pinned

| | |
|---|---|
| Severity | warning |
| Tiers | 1, 2 |
| Stacks | any |
| Scope | each |
| Kind | expr |
| Source | fleetlint-recommended@0.1.0 |

## Requirement

Each stack pins its toolchain in a file the tools read: a `toolchain` directive or a full `go 1.x.y` version in go.mod, `.python-version`, `rust-toolchain.toml`, `.nvmrc` or `.node-version`, the Gradle wrapper, `.fvmrc`; `.tool-versions` or `mise.toml` also count.

## Why

Without a pin, every machine and every CI run builds with whatever compiler or runtime happens to be installed, and failures cannot be reproduced.

## Check

For each `item` in:

```cel
repo.stacks
```

```cel
(item != "go" || scan("go.mod", "^(toolchain go\\d+\\.\\d+|go \\d+\\.\\d+\\.\\d+)").size() > 0) && (item != "python" || file(".python-version") || file(".tool-versions") || file("mise.toml")) && (item != "rust" || file("rust-toolchain.toml") || file("rust-toolchain")) && (item != "node" || file(".nvmrc") || file(".node-version") || file(".tool-versions") || file("mise.toml") || scan("package.json", "\"volta\"\\s*:").size() > 0) && (item != "kotlin" || file("gradle/wrapper/gradle-wrapper.properties")) && (item != "flutter" || file(".fvmrc") || file(".fvm/fvm_config.json") || file(".tool-versions") || file("mise.toml"))
```

## Fix

Add the stack's version file with the version you build with today.

Agent instruction: Add the stack's toolchain pin (go.mod toolchain directive, .python-version, rust-toolchain.toml, .nvmrc, Gradle wrapper, .fvmrc) set to the version currently installed; do not upgrade the toolchain in the same change.

See: docs/baseline.md#hygiene, docs/stacks.md
