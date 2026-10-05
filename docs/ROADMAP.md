# Roadmap

This file tracks what is next and what is blocked. User documentation is at [fleetlint.org](https://fleetlint.org/), from the fleetlint/docs repository.

## Blocked: needs a real runner

**The workflow templates in the catalog repository (`templates/check/` and `templates/*/release.yml`) and fleetlint's own release workflow must run at least once on a real GitHub runner.** They were written from documentation, not from a passing run. Until then they are offered by `fleetlint fix` as a starting point and nothing requires their shape.

Verified locally (2026-10-05, goreleaser 2.18.2, syft 1.52, Go 1.27.1): `.goreleaser.yaml` is valid, and a snapshot release builds all six targets, writes `checksums.txt` and one SPDX SBOM per archive. Not verified anywhere yet: signing with cosign (keyless OIDC and the Rekor upload), `actions/attest-build-provenance`, the workflows themselves on a runner, and Pages deployment. The actions were moved to their latest major versions for 0.1.0 (checkout v7, setup-go v7, cosign-installer v4, goreleaser-action v7, attest-build-provenance v4), so the first run is also the first test of those versions.

To unblock, on a machine with forge access:

1. Push this repository to `github.com/fleetlint/fleetlint`; let `check.yml` run and fix whatever fails. Actions are already pinned with `pinact run`.
2. Add `release.yml` (GoReleaser, cosign keyless, SBOM via syft, provenance attestation) and tag `v0.1.0`. Confirm the release has archives, `checksums.txt`, `*.sigstore.json` bundles and `*.spdx.json`, and that `cosign verify-blob` passes from a clean machine.
3. Update the templates to what actually ran and remove their UNVERIFIED notes. No rule asserts the templates' shape: the release and CI rules check outcomes, and `internal/engine/templates_test.go` keeps the templates passing them.
4. Enable Pages on the `site` repository with `deploy/demo/pages.yml` and point `demo.fleetlint.org` at it.

Gitea: the templates and `scripts/sign.sh` keep their Gitea branches (key-based cosign, the Gitea release API), and the forge client lists Gitea repositories, but no Gitea mirror of this repository is planned, so none of that is verified on a Gitea runner.

## Next, in order

| Version | Contents | Status |
|---|---|---|
| 0.1 | engine, presets, check/init/facts/explain, table/json/agent | done |
| 0.2 | workspace discovery, rule scopes, task-runner contract, `fix` with templates, `.fleetlint.yaml` | done |
| 0.4 (pulled forward) | JSON schema, SARIF and Markdown output, generated rule docs with drift check, prek hook + GitHub Action definitions, `--deep` (runs `make check`), CLI tests, outcome rules with YAML satisfiers (`release/sbom` as reference), `foreach` | done |
| 0.3 | release, signing, changelog and CI families as outcome rules that detect producers | done; presets `strict` and `oss` are open; the templates wait for the runner step above |
| 0.5 | fleet mode: matrix report, history, per-repo agent tasks; `init --pr`; forge discovery with `--from` | done (`--from` is tested against a local server; against the real GitHub API only as far as listing a public owner and starting the clones, and not yet against Gitea or a private organization) |
| 0.6 | `command` rules, baseline ratchet, nested configs | done |
| 0.6.1 | 20 further rules towards the 1.0 catalog (`docs/rules-1.0.md`): repo, lint, quality, docs, deps, ci, release (producer detection), public families; accessors `jobs`, `triggers`, `tags`, `commits`, `grep`, `filesize`, `globmatch`; CEL string/list extensions | done |
| 0.5.1 | public demo at demo.fleetlint.org: `fleetlint fleet` over the four `fleetlint/*` repositories published to Pages nightly (`deploy/demo/`); the report directory already contains `index.html` and HTML action lists | files ready, deploy after the runner step |
| 0.7 | organization layer: locked rules, severity floors, `exceptions: false`, `--require` | done |
| 0.7.1 | git catalog sources (`git+<url>//<path>@<tag or commit>`) | done |
| 0.7.2 | OCI catalog sources with signature verification, private templates shipped with catalogs | needs registry access to test |
| 0.8 | team layer: sources file with aliases, layered resolution, per-layer attribution in reports | done (`signers` in the sources file waits for OCI sources, 0.7.2; the fleet report has a team summary with expiring exceptions and a CSV export) |
| 1.0 | formats frozen, compatibility promise | |

## Also outstanding

- The per-stack release templates (`templates/*/release.yml` in the catalog repository) are unverified like the rest of the release path; the Rust one assumes a `.goreleaser.yaml` with the Rust builder. Folding the four `make dist` variants into one template with a per-stack toolchain step is open.
- Baseline practices without a rule yet: detekt rule strictness for Kotlin, duplication and mutation gates, API-compatibility checks for libraries, signed tags, documented forks.

- Statement coverage is ~44%; CLI and reporters are the gap.
