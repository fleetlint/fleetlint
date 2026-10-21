# Roadmap

This file tracks what is next and what is blocked. User documentation is at [fleetlint.org](https://fleetlint.org/), from the fleetlint/docs repository.

## Verified on a real runner

v0.1.0 (2026-10-06) ran `check.yml` and `release.yml` on GitHub-hosted runners: six archives, `checksums.txt`, a keyless cosign bundle and one SPDX SBOM per archive, plus a build-provenance attestation that `gh attestation verify` accepts. `templates/check/` and `templates/go/release.yml` in the catalog repository match what ran. Still unverified: the other stacks' `release.yml` templates (same shape, different toolchain step) and every Gitea branch (key-based cosign, the Gitea release API) since no Gitea mirror is planned.

## Next, in order

| Version | Contents | Status |
|---|---|---|
| 0.1 | engine, presets, check/init/facts/explain, table/json/agent | done |
| 0.2 | workspace discovery, rule scopes, task-runner contract, `fix` with templates, `.fleetlint.yaml` | done |
| 0.4 (pulled forward) | JSON schema, SARIF and Markdown output, generated rule docs with drift check, prek hook + GitHub Action definitions, `--deep` (runs `make check`), CLI tests, outcome rules with YAML satisfiers (`release/sbom` as reference), `foreach` | done |
| 0.3 | release, signing, changelog and CI families as outcome rules that detect producers | done (presets `strict` and `oss` are in the catalog) |
| 0.5 | fleet mode: matrix report, history, per-repo agent tasks; `init --pr`; forge discovery with `--from` | done (`--from` is tested against a local server; against the real GitHub API only as far as listing a public owner and starting the clones, and not yet against Gitea or a private organization) |
| 0.6 | `command` rules, baseline ratchet, nested configs | done |
| 0.6.1 | 20 further rules towards the 1.0 catalog (`docs/rules-1.0.md`): repo, lint, quality, docs, deps, ci, release (producer detection), public families; accessors `jobs`, `triggers`, `tags`, `commits`, `grep`, `filesize`, `globmatch`; CEL string/list extensions | done |
| 0.6.0 / 0.6.1 | catalogs in import order with free labels, `fleetlint hierarchy`, artifacts as catalog data and the release capabilities keyed by artifact (engine level 5), fleet file includes and nested groups | done (2026-10-21, catalog v0.3.0). The version labels of the rows above are milestones, not tags: OCI catalogs shipped in 0.4.0, the team layer in 0.4 too |
| 0.7 | organization layer: locked rules, severity floors, `exceptions: false`, `--require` | done |
| 0.7.1 | git catalog sources (`git+<url>//<path>@<tag or commit>`) | done |
| 0.7.2 | OCI catalog sources with signature verification, private templates shipped with catalogs | done (verified weekly against the signed `ghcr.io/fleetlint/catalog` tag from go.mod, `TestOCIIntegration`) |
| 0.8 | team layer: sources file with aliases, layered resolution, per-layer attribution in reports | done (`signers` in the sources file waits for OCI sources, 0.7.2; the fleet report has a team summary with expiring exceptions and a CSV export) |
| 0.8 | the format leftovers, adoption and review lows decided on 2026-10-21 (below) | implemented, unreleased (catalog 0.5.0, engine level 7) |
| 1.0 | formats frozen, compatibility promise, conformance tests | after 0.8 |

## Also outstanding

- The per-stack release templates (`templates/*/release.yml` in the catalog repository) other than Go have not run on a runner; the Rust one assumes a `.goreleaser.yaml` with the Rust builder. Folding the four `make dist` variants into one template with a per-stack toolchain step is open.

- Sources for every rule: done in 0.7.0 with catalog 0.4.0. `refs` entries are typed (`{url, kind, section}`, engine level 6), every rule names a spec, guidance, style guide, standard or linter check, or says it is a convention (92 rules, 62 of them with no other source: the slop folklore, the task-runner contract, the not-applicable stack implementations). `TestEveryRuleNamesItsSource` keeps it that way. Open: a periodic link check over the 303 source URLs.

- SARIF (OASIS SARIF 2.1.0, `internal/report/sarif.go`): done in 0.7.0 for locations on every result (pathless findings anchored to the scope's `.fleetlint.yaml` or `README.md`), `helpUri`, `partialFingerprints`, family and `security` tags with `security-severity`, `refs`, `automationDetails.id` per repository, excepted and baselined findings as `suppressions`, rule errors as `invocations[].toolExecutionNotifications`, schema validation in the tests and `sarif/<repository>.sarif` in the fleet report. Done in 0.8: a rule's `locate` puts the expected path on a pathless finding (189 catalog rules; 17 are about the repository as a whole, listed in `internal/catalog/testdata/rules_without_locate.txt`), one run per scope, the expiry on the suppression. Optional `fixes` from the fix templates stay open.

## Decided on 2026-10-21: the road to 1.0

Interview after 0.7.0. The next minor clears everything that still touches a frozen format, then 1.0 freezes.

1. **0.8: format leftovers, then freeze.** Done (unreleased): `combine: any` is gone from the rule format at engine level 7 (`changelog/generator-configured` is one rule, `release/reproducible-build` is keyed by artifact). Artifacts are instances with names ([ADR 0008](adr/0008-artifact-instances.md)). SARIF writes one run per scope (`fleetlint/<repository>/<scope>`), a rule's `locate` puts the expected path on a pathless finding, an exception's expiry is on its suppression. Left as is, on the 2026-10-21 decision: the check JSON's `tool.catalog` names the run's catalog source and `fleet.json`'s top-level `catalog` the version built into the binary (each repository carries its pin); both documents also list the catalogs that applied. 1.0 follows with the compatibility promise and conformance tests.
2. **Adoption in 0.8:** done: `install.sh` is served from fleetlint.org (the docs site copies the release's script at `make gen` and checks it in lint; https with TLS 1.2 only, version validated, checksums matched literally, provenance checked with `gh` when cosign is absent). The `.pre-commit-hooks.yaml` already existed. The action stays as it is, unreleased, docs keep pinning a commit SHA. AUR, Winget and aqua stay out.
3. **Review lows fixed before 1.0:** done: fetch bounds (the sigstore TUF fetch under the run's context with a two-minute limit, at most 8 bundle layers per referrer, git catalog trees at most 16 MiB) and exact `localhost` matching; signers per sources entry and `min_version` against a replayed signed tag; fleet clone and write safety (discovered clones only from the forge's host, cache paths per URL segment, `--deep-clones` and `--allow-dirty`; discovered names were already sanitised); the catalog publish workflow no longer interpolates a step output into `run:`, the deprecated `action/` directory is gone. No environment on the catalog publish workflow: the immutable, admin-only tag rule is the gate (decision 2026-10-21). The docs workflow no longer writes a token into the global git config (stale finding). The remaining lows (fix-template provenance in the plan, exception pattern validation, baseline duplicates, history compaction, the worker pool, scope tier inference, GitLab in the forge client) stay open after 1.0.

## Decided on 2026-10-20: hierarchy and artifacts

- **Catalog order and labels** ([ADR 0006](adr/0006-catalog-order-by-import.md)): the fixed layers are gone; catalogs apply in the order includes and extends list them, labels are free, `fleetlint hierarchy` and every report list the catalogs with their pins, fleet files include fleet files and groups nest by path. Open: nothing before 1.0.
- **Release checks per artifact** ([ADR 0007](adr/0007-artifacts-as-catalog-data.md)): artifacts are catalog data (`artifacts/<id>.yaml`), the four release capabilities are keyed by artifact (`by: artifact`), engine level 5. Open: step-level `if` guards and reusable workflows (answered by `facts.artifacts` for now). Instances of one kind and the removal of `combine: any` are done for 0.8 ([ADR 0008](adr/0008-artifact-instances.md), engine level 7, catalog 0.5.0).

## Backlog: what large organisations will ask for

Gaps noted on 2026-10-06 against what exists today. The forge client lists repositories and looks up visibility only; the fleet history is a directory of dated JSON files; six stacks are detected; `init --pr` opens a pull request but nothing runs on a schedule.

Running at scale
- A scheduled central runner: nightly `fleet` over an owner, durable results, pull requests for fixable findings, retries. API-only evaluation for cheap facts so not every repository is cloned.
- `fix --apply --pr` and `fleet --fix --pr`: when main is protected, commit the fixes on a branch and open a pull request on GitHub, GitLab, Gitea or Forgejo through the forge API (`init --pr` does this for the config only, and only on GitHub and Gitea; GitLab is not in the forge client at all). Detect protection instead of failing on push, reuse an open fleetlint branch, and write the findings into the PR body.
- Trend output per team and rule (time series or a stable export for a warehouse), built on the dated history files.
- Ownership from `CODEOWNERS`, forge teams or a service catalog (Backstage) instead of the hand-kept fleet file; the `repo/codeowners` rule only checks presence.
- Exception governance: who may grant one, approver recorded, escalation when they expire, policy layers that forbid exceptions per rule (locks cover the rule side, not the human side).

Security and supply chain
- A `forge/*` family for repository settings read through the API: branch protection, required reviews, signed commits, secret scanning and vulnerability alerts enabled, deploy keys, admin list. This is the Scorecard overlap the comparison page exposes.
- Verification beyond presence: stated SLSA level and attestations checked, lockfile integrity, fork pull-request workflows without write tokens, allowed-actions lists, dependency review on pull requests (`ci/pull-request-target` exists; the rest does not).
- Signed catalogs with provenance verified before a catalog may write files (0.7.2 covers OCI; git sources have no signature check).
- Key-based verification as an alternative to keyless OIDC: verify git catalog commits and tags (SSH or GPG signatures against allowed signers) and OCI catalog images (cosign public keys) with signing keys listed in the sources file, for registries and forges without a sigstore OIDC issuer and for air-gapped setups.

Integration
- Pull-request annotations or review comments, incremental against the base branch (the action only runs `check`).
- Scorecard-compatible JSON, Backstage tech-insights facts, Slack or e-mail digest of the fleet summary (SARIF and Markdown exist).
- A read-only policy endpoint answering "does repository X pass policy Y" from the latest fleet run, for merge or creation gates.

Repository shapes
- Build graphs: Bazel, Nx, Turborepo (`taskrunner` knows make, just, task, npm scripts, gradle).
- Stacks: Java/Maven, .NET, C/C++, Ruby, PHP; hygiene rules for Terraform, Helm, Kustomize, protobuf and OpenAPI.

Governance of fleetlint
- Deprecation policy, schema version in every output, conformance tests for third-party catalogs (1.0 row above).
- Kubernetes-project conventions: `OWNERS`, `SECURITY_CONTACTS`, DCO or CLA check, published support matrix.

Adoption (since 0.2.0: Homebrew tap, Scoop bucket, Nix flake in `fleetlint/nur`, `.deb`/`.rpm`/`.apk`/Arch packages, the signed image `ghcr.io/fleetlint/fleetlint`, `fleetlint/action` (unreleased), `scripts/install.sh` and a GitLab CI job in `ci/gitlab/`. Still open below: AUR, Winget, the aqua registry entry, the first action release and its Marketplace listing, `install.sh` served from fleetlint.org, a GitLab CI/CD catalog component)
- `install.sh` served from fleetlint.org (`curl -sSfL https://fleetlint.org/install.sh | sh`): picks OS and arch, downloads the release archive, verifies `checksums.txt` against the cosign bundle, installs to `~/.local/bin` or a given prefix, pinnable with a version argument.
- Package managers from GoReleaser: Homebrew tap (`brew install fleetlint/tap/fleetlint`), Scoop and Winget for Windows, `.deb`/`.rpm`/`.apk` via nfpm, AUR, a Nix flake, and a `mise`/`aqua` registry entry so `mise use fleetlint` works.
- A published GitHub Action (`fleetlint/action` on the Marketplace) and a GitLab CI component that install a pinned release, run `check`, upload SARIF to code scanning and annotate pull requests; a Gitea/Forgejo variant of the same.
- A pre-commit / prek hook definition in the fleetlint repository so `repo: https://github.com/fleetlint/fleetlint` works in `.pre-commit-config.yaml`.
- Container image (`ghcr.io/fleetlint/fleetlint`, distroless, signed, SBOM attached) for CI systems that run everything in containers.
- Renovate and Dependabot know the install paths (Homebrew formula, action tag, image tag, `install.sh` version pin) so adopters' pins stay current.
- `fleetlint init` as the five-minute path: `curl … | sh && fleetlint init --pr` should be the whole quick start on the home page, with a copy button.

Fleet app without a server
- A GitHub App ("fleetlint for GitHub") whose only runtime is a scheduled workflow in one repository, the way self-hosted Renovate runs from `renovatebot/github-action`: the workflow mints installation tokens from the App's private key (`actions/create-github-app-token`), lists the installations and their repositories, runs `fleet` with API-only facts where possible, opens or updates fix pull requests and publishes the matrix to Pages. Installing the App is the whole onboarding: no `.fleetlint.yaml` edit, no secret in the target repository, no server. What a server would add and this cannot: reacting to webhooks (push, PR opened) instead of a schedule; a push-to-main re-check can be approximated by the composite action in the target repository. The same shape works on Gitea/Forgejo with a bot account and a cron workflow; GitLab needs a group access token and a scheduled pipeline.

## Low findings from the 0.3 security and architecture review (for later decisions)

The review on 2026-10-12 found these; 0.3.0 fixed everything above low. Each line is a decision to make, not a commitment.

Security
- Catalog fetching: the sigstore TUF fetch ignores the run's timeout; OCI referrers and bundle layers are unbounded in count; a git catalog fetch is bounded by time only, not by size on disk.
- `localhost*` gets plain HTTP and Docker credentials for OCI (`localhost.evil.tld` matches): compare the host exactly.
- Signers apply to every `oci://` reference, not per sources alias; nothing protects against an old signed tag being served again (no minimum version).
- Fix templates from remote catalogs overlay the built-ins and become hooks and workflows: show each template's catalog in the fix plan.
- Fleet clones: the forge may hand out a local path or another host as clone URL (the token no longer follows it); cache directories of two repositories can collide (`a/b_c` and `a_b/c`); discovered repository names are not cleaned like fleet-file names (`#` or `?` break the action-page links).
- `fleet --deep` with `--from` runs `make check` in every cloned repository: refuse it for clones unless allowed per entry? `fleet --fix --apply` writes into checkouts with uncommitted changes: refuse without `--allow-dirty`?
- `init` writes workspace member names unescaped into a YAML comment.
- Exception `re:` patterns are not validated when the config loads; an invalid one never matches. Baseline fingerprints are a set, so a new duplicate of a baselined finding stays hidden.
- `scripts/install.sh` and the action: without cosign only the checksum from the same release is checked (`gh attestation verify` would check provenance); add `curl --proto '=https' --tlsv1.2`, validate the version argument, match the checksum line with `grep -F`.
- CI and release: the docs workflow writes `GITHUB_TOKEN` into the global git config for later steps; the catalog's publish workflow has no environment or tag protection and interpolates a step output into `run:`; the deprecated `action/` interpolates its inputs into shell (delete it); repository settings `sha_pinning_required` and an allowed-actions list; rulesets limiting the package repositories' deploy keys to fast-forward pushes; the Homebrew cask removes the quarantine flag (notarize instead?); the image sets `safe.directory=*` (use `/repo`).

Architecture
- Fleet history stores a full `fleet.json` per run, dry runs included: compact history files or `--keep N`.
- Fleet runs one repository at a time and a panic in one rule ends the run: a worker pool with per-repository recovery (the CLI no longer holds state that blocks it).
- A scope re-infers release and CI on its own, so a member of a tier-1 repository with root-only workflows is inferred as tier 3; forge-reported visibility is labelled `detected`.
- `Repo` keeps the context it was opened with; test seams `requireSCT` and `loadBundle` are package variables.
- GitLab is missing from the forge client (`--from gitlab:`, visibility, `init --pr`).
- Catalog fix: what the top-level `catalog` field of the check JSON and of `fleet.json` means differs (the run's pinned version vs the built-in one); per-repository pins are now in `fleet.json`.
