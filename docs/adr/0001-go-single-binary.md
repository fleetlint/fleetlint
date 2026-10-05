# 0001 — Go, single static binary

Date: 2026-10-04. Status: accepted.

## Context
The tool must install with one command on developer machines and CI runners of several stacks, run in a pre-commit hook in well under a second, and embed its rule catalogs. Rust and Go both deliver a static binary; Python was excluded for needing a runtime.

## Decision
Go. `embed` ships catalogs and templates inside the binary, cross-compilation is a flag, GoReleaser covers signing/SBOM/releases natively, and the edit-test loop is fastest for a tool whose complexity is in rules rather than in performance-critical code.

## Consequences
Binary size is around 14 MB because of CEL's dependencies (ANTLR, protobuf). Accepted; `expr-lang/expr` would be smaller but is not a standard. Rust remains the better choice for anything CPU-bound, which this is not.
