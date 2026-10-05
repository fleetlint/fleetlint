# Roadmap

This file tracks what is next and what is blocked. User documentation is at [fleetlint.org](https://fleetlint.org/), from the fleetlint/docs repository.

## Verified on a real runner

v0.1.0 (2026-10-06) ran `check.yml` and `release.yml` on GitHub-hosted runners: six archives, `checksums.txt`, a keyless cosign bundle and one SPDX SBOM per archive, plus a build-provenance attestation that `gh attestation verify` accepts. `templates/check/` and `templates/go/release.yml` in the catalog repository match what ran. Still unverified: the other stacks' `release.yml` templates (same shape, different toolchain step), Pages deployment of `deploy/demo/pages.yml`, and every Gitea branch (key-based cosign, the Gitea release API) since no Gitea mirror is planned.

## Next, in order

| Version | Contents | Status |
|---|---|---|
| 0.1 | engine, presets, check/init/facts/explain, table/json/agent | done |
| 0.2 | workspace discovery, rule scopes, task-runner contract, `fix` with templates, `.fleetlint.yaml` | done |
| 0.4 (pulled forward) | JSON schema, SARIF and Markdown output, generated rule docs with drift check, prek hook + GitHub Action definitions, `--deep` (runs `make check`), CLI tests, outcome rules with YAML satisfiers (`release/sbom` as reference), `foreach` | done |
| 0.3 | release, signing, changelog and CI families as outcome rules that detect producers | done; presets `strict` and `oss` are open |
| 0.5 | fleet mode: matrix report, history, per-repo agent tasks; `init --pr`; forge discovery with `--from` | done (`--from` is tested against a local server; against the real GitHub API only as far as listing a public owner and starting the clones, and not yet against Gitea or a private organization) |
| 0.6 | `command` rules, baseline ratchet, nested configs | done |
| 0.6.1 | 20 further rules towards the 1.0 catalog (`docs/rules-1.0.md`): repo, lint, quality, docs, deps, ci, release (producer detection), public families; accessors `jobs`, `triggers`, `tags`, `commits`, `grep`, `filesize`, `globmatch`; CEL string/list extensions | done |
| 0.5.1 | public demo at demo.fleetlint.org: `fleetlint fleet` over the four `fleetlint/*` repositories published to Pages nightly (`deploy/demo/`); the report directory already contains `index.html` and HTML action lists | files ready, Pages deployment open |
| 0.7 | organization layer: locked rules, severity floors, `exceptions: false`, `--require` | done |
| 0.7.1 | git catalog sources (`git+<url>//<path>@<tag or commit>`) | done |
| 0.7.2 | OCI catalog sources with signature verification, private templates shipped with catalogs | needs registry access to test |
| 0.8 | team layer: sources file with aliases, layered resolution, per-layer attribution in reports | done (`signers` in the sources file waits for OCI sources, 0.7.2; the fleet report has a team summary with expiring exceptions and a CSV export) |
| 1.0 | formats frozen, compatibility promise | |

## Also outstanding

- The per-stack release templates (`templates/*/release.yml` in the catalog repository) other than Go have not run on a runner; the Rust one assumes a `.goreleaser.yaml` with the Rust builder. Folding the four `make dist` variants into one template with a per-stack toolchain step is open.
- Baseline practices without a rule yet: detekt rule strictness for Kotlin, duplication and mutation gates, API-compatibility checks for libraries, signed tags, documented forks.

- Statement coverage is ~44%; CLI and reporters are the gap.
