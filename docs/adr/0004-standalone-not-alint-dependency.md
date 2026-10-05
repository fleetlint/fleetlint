# 0004 — Standalone; alint's ideas, not its binary

Date: 2026-10-04. Status: accepted.

## Context
alint (Rust, pre-1.0) already covers the file-shape layer well: facts, bounded `when`, `extends` with SRI and a trust boundary, a `command` kind, baselines, agent instructions. It lacks outcome rules, reasoned exceptions with deadlines, profiles, Makefile and Gitea awareness, and a fleet view.

## Decision
fleetlint is self-contained. It adopts the good designs (facts, digest-pinned extends, trust boundary, agent instructions, nested scopes) and implements its own shape rules with CEL. alint may become an optional provider later if its config format stabilizes.

## Consequences
More code to own for shape rules; no runtime dependency on a fast-moving pre-1.0 tool; one install, one config file.
