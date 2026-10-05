# 0005 — Catalog trust boundary

Date: 2026-10-04. Status: accepted.

## Context
Catalogs come from three places: presets in the binary, local files in the repo, and https URLs. A remote catalog is code someone else controls.

## Decision
Remote catalogs must be pinned by sha256 digest and are untrusted: they may add rules and tighten them but cannot declare `command` rules, and their includes may only reference presets or other digest-pinned https catalogs. Only the repository's own `.fleetlint.yaml` and the local files it points to are trusted. Disabling a rule, lowering a severity and adding exceptions are only possible in the repo's own file, and each requires a reason that the report shows.

## Consequences
A compromised catalog server can at worst make checks stricter or fail to load. Weakening is always visible and local.
